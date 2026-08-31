package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/mask/ai/doubao-web-api/internal/account"
	"github.com/mask/ai/doubao-web-api/internal/cdp"
	"github.com/mask/ai/doubao-web-api/internal/chrome"
	"github.com/mask/ai/doubao-web-api/internal/config"
	"github.com/mask/ai/doubao-web-api/internal/doubao"
	"github.com/mask/ai/doubao-web-api/internal/pool"
	"github.com/mask/ai/doubao-web-api/internal/storage"
	"github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

const maxUploadSize = 10 << 20      // 10MB
const maxMediaUploadSize = 30 << 20 // 30MB for audio

type doubaoClient interface {
	GenerateImage(ctx context.Context, opts doubao.GenerateImageOptions) ([]doubao.ImageResult, error)
	GenerateVideo(ctx context.Context, opts doubao.GenerateVideoOptions) ([]doubao.VideoResult, error)
	UploadImage(ctx context.Context, data []byte, filename string) (doubao.UploadResult, error)
	UploadMedia(ctx context.Context, data []byte, filename string) (doubao.UploadResult, error)
	ExtractRefImageKey(ctx context.Context, image any) (string, error)
	ExtractVideoContent(ctx context.Context, items []*model.CreateContentGenerationContentItem) (doubao.ExtractedVideoContent, error)
}

type chromeRestarter interface {
	Restart(ctx context.Context) error
}

type Server struct {
	cfg                  config.Config
	doubao               doubaoClient
	accounts             *account.Store
	chrome               chromeRestarter
	workers              *pool.Pool
	cos                  *storage.COS
	videoTasks           *videoTaskStore
	localMedia           *localMediaStore
	sessionSwitchPending atomic.Bool
}

func New(cfg config.Config, client doubaoClient, accounts *account.Store, chrome chromeRestarter, cos *storage.COS) *Server {
	return &Server{
		cfg:        cfg,
		doubao:     client,
		accounts:   accounts,
		chrome:     chrome,
		cos:        cos,
		videoTasks: newVideoTaskStore(),
		localMedia: newLocalMediaStore(),
	}
}

// SetPool attaches the multi-Chrome worker pool used for concurrent video gens.
func (s *Server) SetPool(p *pool.Pool) {
	s.workers = p
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /api/v3/images/proxy", s.handleImageProxy)
	mux.HandleFunc("POST /api/v3/images/uploads", s.handleImageUpload)
	mux.HandleFunc("POST /api/v3/files/uploads", s.handleFileUpload)
	mux.HandleFunc("POST /api/v3/images/generations", s.handleGenerateImages)
	mux.HandleFunc("POST /api/v3/contents/generations/tasks", s.handleCreateContentGenerationTask)
	mux.HandleFunc("GET /api/v3/contents/generations/tasks/{id}", s.handleGetContentGenerationTask)
	s.registerAdminRoutes(mux)
	return mux
}

func (s *Server) markSessionSwitchPending() {
	s.sessionSwitchPending.Store(true)
}

func (s *Server) isSessionSwitchPending() bool {
	return s.sessionSwitchPending.Load()
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{
		"status":       "ok",
		"max_parallel": s.cfg.MaxParallelVideo,
	}
	if s.workers != nil {
		out["workers"] = s.workers.Workers()
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleImageUpload(w http.ResponseWriter, r *http.Request) {
	if err := s.checkAuth(r); err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart form: "+err.Error())
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		file, header, err = r.FormFile("image")
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing file field (use file or image)")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "read file: "+err.Error())
		return
	}

	filename := header.Filename
	if filename == "" {
		filename = "image.png"
	}

	result, err := s.doubao.UploadImage(r.Context(), data, filename)
	if err != nil {
		log.Printf("upload image failed: %v", err)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	s.localMedia.put(result.URI, data, filename, result.Format)

	cdnURL := result.URL
	if cdnURL != "" {
		cdnURL = s.buildProxyURL(r, cdnURL)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":     result.URI,
		"object": "image.upload",
		"uri":    result.URI,
		"url":    cdnURL,
		"name":   result.Name,
		"format": result.Format,
	})
}

func (s *Server) handleFileUpload(w http.ResponseWriter, r *http.Request) {
	if err := s.checkAuth(r); err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxMediaUploadSize)
	if err := r.ParseMultipartForm(maxMediaUploadSize); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart form: "+err.Error())
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing file field")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "read file: "+err.Error())
		return
	}

	filename := header.Filename
	if filename == "" {
		filename = "file.bin"
	}

	if doubao.IsAudioFile(filename) {
		id := uuid.New().String()
		uri := localAudioPrefix + id
		s.localMedia.put(id, data, filename, doubao.MediaExt(filename))
		writeJSON(w, http.StatusOK, map[string]any{
			"id":     uri,
			"object": "audio.upload",
			"uri":    uri,
			"url":    "",
			"name":   filename,
			"format": doubao.MediaExt(filename),
		})
		return
	}

	result, err := s.doubao.UploadMedia(r.Context(), data, filename)
	if err != nil {
		log.Printf("upload media failed: %v", err)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	s.localMedia.put(result.URI, data, filename, result.Format)

	cdnURL := result.URL
	if cdnURL != "" {
		cdnURL = s.buildProxyURL(r, cdnURL)
	}

	objectType := "file.upload"
	if doubao.IsAudioFile(filename) {
		objectType = "audio.upload"
	} else {
		objectType = "image.upload"
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":     result.URI,
		"object": objectType,
		"uri":    result.URI,
		"url":    cdnURL,
		"name":   result.Name,
		"format": result.Format,
	})
}

func (s *Server) handleGenerateImages(w http.ResponseWriter, r *http.Request) {
	if err := s.checkAuth(r); err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}

	var req model.GenerateImagesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		writeError(w, http.StatusBadRequest, "prompt is required")
		return
	}

	modelName := req.Model
	if modelName == "" {
		modelName = "doubao-seedream-5-0"
	}

	ratio := ""
	if req.Size != nil && *req.Size != "" {
		ratio = doubao.SizeToRatio(*req.Size)
	}

	ctx := r.Context()
	refKey, err := s.doubao.ExtractRefImageKey(ctx, req.Image)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	images, err := s.doubao.GenerateImage(ctx, doubao.GenerateImageOptions{
		Prompt:      req.Prompt,
		Ratio:       ratio,
		RefImageKey: refKey,
		Timeout:     s.cfg.RequestTimeout,
	})
	if err != nil {
		log.Printf("generate image failed: %v", err)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	responseFormat := model.GenerateImagesResponseFormatURL
	if req.ResponseFormat != nil {
		responseFormat = *req.ResponseFormat
	}
	if responseFormat == model.GenerateImagesResponseFormatBase64 {
		writeError(w, http.StatusBadRequest, "b64_json is not supported yet, use response_format=url")
		return
	}

	data := make([]*model.Image, 0, len(images))
	for _, img := range images {
		imageURL := img.URL
		proxyURL := s.buildProxyURL(r, imageURL)
		size := ratioToSize(ratio)
		data = append(data, &model.Image{
			Url:  &proxyURL,
			Size: size,
		})
	}

	s.recordImageGeneration(r, req.Prompt, modelName, images)

	resp := model.ImagesResponse{
		Model:   modelName,
		Created: time.Now().Unix(),
		Data:    data,
		Usage: &model.GenerateImagesUsage{
			GeneratedImages: int64(len(data)),
		},
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleCreateContentGenerationTask(w http.ResponseWriter, r *http.Request) {
	if err := s.checkAuth(r); err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}

	raw, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body: "+err.Error())
		return
	}
	log.Printf("create video task raw request: %s", truncateForLog(string(raw), 8192))

	var req model.CreateContentGenerationTaskRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if len(req.Content) == 0 {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	ctx := r.Context()
	content, err := s.doubao.ExtractVideoContent(ctx, req.Content)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	prompt := strings.TrimSpace(content.Prompt)
	log.Printf("create video task extracted: prompt_len=%d images=%d has_audio=%v model=%q",
		len(prompt), len(content.RefImageKeys), content.RefAudioKey != "", req.Model)
	if prompt == "" {
		writeError(w, http.StatusBadRequest,
			"text prompt is required in content (got images/audio only; refuse video generation without text)")
		return
	}
	content.Prompt = prompt

	modelName := req.Model
	if modelName == "" {
		modelName = "doubao-seedance-2-0-fast"
	}

	ratio := ""
	if req.Ratio != nil && *req.Ratio != "" {
		ratio = *req.Ratio
	}

	taskID := uuid.New().String()
	s.videoTasks.create(taskID, modelName, ratio, req.Duration)
	log.Printf("create video task accepted (task=%s, prompt_preview=%q)", taskID, truncateForLog(prompt, 120))

	go s.runVideoGeneration(taskID, content, ratio, modelName, req.Duration)

	writeJSON(w, http.StatusOK, model.CreateContentGenerationTaskResponse{
		ID: taskID,
	})
}

func (s *Server) handleGetContentGenerationTask(w http.ResponseWriter, r *http.Request) {
	if err := s.checkAuth(r); err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}

	taskID := r.PathValue("id")
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "task id is required")
		return
	}

	task, ok := s.videoTasks.get(taskID)
	if !ok {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}

	resp := model.GetContentGenerationTaskResponse{
		ID:        task.ID,
		Model:     task.Model,
		Status:    task.Status,
		Ratio:     optionalString(task.Ratio),
		Duration:  task.Duration,
		CreatedAt: task.CreatedAt,
		UpdatedAt: task.UpdatedAt,
		Usage:     model.Usage{},
	}
	if task.Error != nil {
		resp.Error = task.Error
	}
	if task.VideoURL != "" {
		videoURL := task.VideoURL
		if s.cos == nil || !s.cos.IsPublicURL(videoURL) {
			videoURL = s.buildVideoProxyURL(r, videoURL)
		}
		resp.Content = model.Content{VideoURL: videoURL}
	}

	writeJSON(w, http.StatusOK, struct {
		model.GetContentGenerationTaskResponse
		ETAText    string `json:"eta_text,omitempty"`
		ETAMinutes int    `json:"eta_minutes,omitempty"`
	}{
		GetContentGenerationTaskResponse: resp,
		ETAText:                          task.ETAText,
		ETAMinutes:                       task.ETAMinutes,
	})
}

func (s *Server) runVideoGeneration(taskID string, content doubao.ExtractedVideoContent, ratio, modelName string, duration *int64) {
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.VideoTimeout)
	defer cancel()

	s.videoTasks.update(taskID, func(task *videoTaskRecord) {
		task.Status = model.StatusRunning
	})

	var audioData []byte
	var audioName string
	var imageFiles []doubao.MediaFile
	for _, key := range content.RefImageKeys {
		entry, ok := s.localMedia.take(key)
		if !ok {
			s.videoTasks.update(taskID, func(task *videoTaskRecord) {
				task.Status = model.StatusFailed
				task.Error = &model.ContentGenerationError{
					Code:    "generation_failed",
					Message: "image reference expired or not found, re-upload images",
				}
			})
			return
		}
		imageFiles = append(imageFiles, doubao.MediaFile{Data: entry.Data, Filename: entry.Filename})
	}
	if doubao.IsLocalAudioRef(content.RefAudioKey) {
		entry, ok := s.localMedia.take(content.RefAudioKey)
		if !ok {
			s.videoTasks.update(taskID, func(task *videoTaskRecord) {
				task.Status = model.StatusFailed
				task.Error = &model.ContentGenerationError{
					Code:    "generation_failed",
					Message: "local audio reference expired or not found, re-upload mp3",
				}
			})
			return
		}
		audioData = entry.Data
		audioName = entry.Filename
	}

	var videoDuration int64
	if duration != nil && *duration > 0 {
		videoDuration = int64(cdp.NormalizeVideoDurationSec(int(*duration)))
		if videoDuration != *duration {
			log.Printf("generate video: duration %d remapped to %ds", *duration, videoDuration)
		}
	}

	genOpts := doubao.GenerateVideoOptions{
		Prompt:           content.Prompt,
		Ratio:            ratio,
		RefImageKeys:     content.RefImageKeys,
		RefImageFiles:    imageFiles,
		RefAudioKey:      content.RefAudioKey,
		RefAudioData:     audioData,
		RefAudioFilename: audioName,
		Timeout:          s.cfg.VideoTimeout,
		Duration:         videoDuration,
		Model:            modelName,
		OnETA: func(text string, minutes int) {
			s.videoTasks.update(taskID, func(task *videoTaskRecord) {
				task.ETAText = text
				task.ETAMinutes = minutes
			})
			log.Printf("generate video: doubao eta %q (%d min, task=%s)", text, minutes, taskID)
		},
	}

	videos, accountID, err := s.runVideoOnPool(ctx, taskID, genOpts)
	if err != nil {
		log.Printf("generate video failed (task=%s): %v", taskID, err)
		code := "generation_failed"
		message := sanitizeErrorMessage(err.Error())
		if errors.Is(err, account.ErrNoQuotaAvailable) || errors.Is(err, account.ErrNotFound) {
			code = "quota_exhausted"
		}
		var quotaErr *cdp.VideoQuotaExceededError
		if errors.As(err, &quotaErr) {
			code = "quota_exhausted"
			message = quotaErr.Error()
		}
		var genFailErr *cdp.VideoGenerationFailedError
		if errors.As(err, &genFailErr) {
			if genFailErr.Code != "" {
				code = genFailErr.Code
			}
			message = genFailErr.Error()
		}
		s.videoTasks.update(taskID, func(task *videoTaskRecord) {
			task.Status = model.StatusFailed
			task.Error = &model.ContentGenerationError{
				Code:    code,
				Message: message,
			}
		})
		return
	}

	finalURL := videos[0].VideoURL
	if s.cos != nil && s.cos.Enabled() {
		if cosURL, err := s.uploadVideoToCOS(ctx, taskID, finalURL); err != nil {
			log.Printf("COS upload failed (task=%s), falling back to proxy URL: %v", taskID, err)
		} else {
			finalURL = cosURL
			log.Printf("video uploaded to COS (task=%s): %s", taskID, cosURL)
		}
	}

	s.videoTasks.update(taskID, func(task *videoTaskRecord) {
		task.Status = model.StatusSucceeded
		task.VideoURL = finalURL
		if videos[0].Duration > 0 && task.Duration == nil {
			d := int64(videos[0].Duration)
			task.Duration = &d
		}
	})
	log.Printf("video generation succeeded (task=%s, model=%s, account=%d)", taskID, modelName, accountID)
	s.recordVideoGeneration(taskID, content.Prompt, modelName, finalURL, imageFiles, accountID)
	s.consumeVideoQuotaOnSuccess(accountID, modelName)
}

// runVideoOnPool leases a Chrome worker, generates, and on UI quota exhaustion
// releases and retries once on another worker.
func (s *Server) runVideoOnPool(ctx context.Context, taskID string, genOpts doubao.GenerateVideoOptions) ([]doubao.VideoResult, int64, error) {
	if s.workers == nil {
		videos, err := s.doubao.GenerateVideo(ctx, genOpts)
		return videos, 0, err
	}

	worker, err := s.workers.Acquire(ctx)
	if err != nil {
		return nil, 0, err
	}
	accountID := worker.AccountID
	log.Printf("generate video: leased worker account=%s id=%d port=%d session=%q (task=%s) — watch THIS Chrome window",
		worker.Name, worker.AccountID, worker.CDPPort, worker.SessionDir, taskID)
	chrome.BringToFront(worker.CDPPort)

	videos, err := generateVideoOnBrowser(ctx, worker.Browser, genOpts)
	if err != nil {
		var quotaErr *cdp.VideoQuotaExceededError
		if errors.As(err, &quotaErr) && s.accounts != nil && worker.AccountID != 0 {
			sw, _ := s.accounts.MarkExhausted(worker.AccountID)
			if sw.Message != "" {
				log.Printf("generate video: %s (task=%s)", sw.Message, taskID)
			}
			s.workers.Release(worker)
			worker = nil

			retryWorker, acquireErr := s.workers.Acquire(ctx)
			if acquireErr != nil {
				return nil, accountID, err
			}
			accountID = retryWorker.AccountID
			log.Printf("generate video: quota exhausted, retrying on account=%s id=%d (task=%s)",
				retryWorker.Name, retryWorker.AccountID, taskID)
			videos, err = generateVideoOnBrowser(ctx, retryWorker.Browser, genOpts)
			if err != nil {
				var retryQuota *cdp.VideoQuotaExceededError
				if errors.As(err, &retryQuota) && retryWorker.AccountID != 0 {
					_, _ = s.accounts.MarkExhausted(retryWorker.AccountID)
				}
				s.workers.Release(retryWorker)
				return nil, accountID, err
			}
			s.workers.Release(retryWorker)
			return videos, accountID, nil
		}
		s.workers.Release(worker)
		return nil, accountID, err
	}
	s.workers.Release(worker)
	return videos, accountID, nil
}

func generateVideoOnBrowser(ctx context.Context, browser *cdp.Browser, opts doubao.GenerateVideoOptions) ([]doubao.VideoResult, error) {
	if browser == nil {
		return nil, fmt.Errorf("no browser")
	}
	imageFiles := make([]cdp.LocalMediaFile, 0, len(opts.RefImageFiles))
	for _, f := range opts.RefImageFiles {
		imageFiles = append(imageFiles, cdp.LocalMediaFile{Data: f.Data, Filename: f.Filename})
	}
	uiOpts := cdp.VideoUIOptions{
		Prompt:           opts.Prompt,
		Ratio:            opts.Ratio,
		RefImageKeys:     opts.RefImageKeys,
		RefImageFiles:    imageFiles,
		RefAudioKey:      opts.RefAudioKey,
		RefAudioData:     opts.RefAudioData,
		RefAudioFilename: opts.RefAudioFilename,
		Timeout:          opts.Timeout,
		Duration:         opts.Duration,
		Model:            opts.Model,
	}
	if opts.OnETA != nil {
		uiOpts.OnETA = func(eta cdp.VideoETA) {
			opts.OnETA(eta.Text, eta.Minutes)
		}
	}
	items, err := browser.GenerateVideoViaUI(ctx, uiOpts)
	if err != nil {
		return nil, err
	}
	out := make([]doubao.VideoResult, 0, len(items))
	for _, item := range items {
		out = append(out, doubao.VideoResult{
			VideoURL: item.VideoURL,
			CoverURL: item.CoverURL,
			Width:    item.Width,
			Height:   item.Height,
			Duration: item.Duration,
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("video UI generation returned no videos")
	}
	return out, nil
}

func (s *Server) uploadVideoToCOS(ctx context.Context, taskID, rawURL string) (string, error) {
	data, err := s.fetchMediaBytes(ctx, rawURL)
	if err != nil {
		return "", err
	}
	key := s.cos.VideoKey(taskID)
	if err := s.cos.Put(key, data, "video/mp4"); err != nil {
		return "", err
	}
	return s.cos.PublicURL(key), nil
}

func (s *Server) fetchMediaBytes(ctx context.Context, rawURL string) ([]byte, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || !isAllowedMediaHost(parsed) {
		return nil, fmt.Errorf("invalid media url")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "https://www.doubao.com/chat/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	client := &http.Client{Timeout: 3 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream returned %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 80<<20))
}

// consumeVideoQuotaOnSuccess decrements Seedance remaining for the leased account.
// Mini costs 1; Fast costs 2 (clamped to remaining so Fast can run with 1 left).
func (s *Server) consumeVideoQuotaOnSuccess(accountID int64, modelName string) {
	if s.accounts == nil {
		return
	}
	if accountID != 0 {
		sw, err := s.accounts.ConsumeOnSuccess(accountID, modelName)
		if err != nil {
			log.Printf("account video quota: %v", err)
			return
		}
		if sw.Message != "" {
			log.Printf("account video quota: %s", sw.Message)
		}
		return
	}
	// Legacy single-Chrome path (no pool account binding).
	sw, err := s.accounts.ConsumeFastOnSuccess(modelName)
	if err != nil {
		log.Printf("account video quota: %v", err)
		return
	}
	if sw.Message != "" {
		log.Printf("account video quota: %s", sw.Message)
	}
	if sw.Switched {
		s.restartDefaultChrome(fmt.Sprintf("auto-switch to %s", sw.ToName))
	}
}

func (s *Server) restartDefaultChrome(reason string) {
	if !s.cfg.AutoRestartChrome || s.chrome == nil {
		s.markSessionSwitchPending()
		log.Printf("chrome: auto-restart disabled; restart Chrome manually (%s)", reason)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	log.Printf("chrome: auto-restart default worker (%s)", reason)
	if err := s.chrome.Restart(ctx); err != nil {
		s.markSessionSwitchPending()
		log.Printf("chrome: auto-restart failed: %v — please restart manually", err)
		return
	}
	s.sessionSwitchPending.Store(false)
	log.Printf("chrome: auto-restart ok (%s)", reason)
}

func (s *Server) handleImageProxy(w http.ResponseWriter, r *http.Request) {
	rawURL := r.URL.Query().Get("url")
	if rawURL == "" {
		writeError(w, http.StatusBadRequest, "url query parameter is required")
		return
	}

	parsed, err := url.Parse(rawURL)
	if err != nil || !isAllowedMediaHost(parsed) {
		writeError(w, http.StatusBadRequest, "invalid media url")
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, rawURL, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	req.Header.Set("Referer", "https://www.doubao.com/chat/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "fetch image: "+err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("upstream returned %d", resp.StatusCode))
		return
	}

	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	} else {
		w.Header().Set("Content-Type", "image/png")
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, resp.Body); err != nil {
		log.Printf("proxy image copy failed: %v", err)
	}
}

func (s *Server) buildProxyURL(r *http.Request, imageURL string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host := r.Host
	if host == "" {
		host = "127.0.0.1:" + s.cfg.Port
	}
	return fmt.Sprintf("%s://%s/api/v3/images/proxy?url=%s", scheme, host, url.QueryEscape(imageURL))
}

func (s *Server) buildVideoProxyURL(r *http.Request, videoURL string) string {
	return s.buildProxyURL(r, videoURL)
}

func isAllowedMediaHost(u *url.URL) bool {
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return strings.HasSuffix(host, "byteimg.com") ||
		strings.HasSuffix(host, "bytecdn.cn") ||
		strings.HasSuffix(host, "ibytedtos.com") ||
		strings.HasSuffix(host, "douyinstatic.com") ||
		strings.HasSuffix(host, "douyinvod.com") ||
		strings.Contains(host, "douyinvod.com") ||
		strings.HasSuffix(host, "douyin.com") ||
		strings.HasSuffix(host, "doubao.com") ||
		strings.HasSuffix(host, "snssdk.com") ||
		strings.Contains(host, "tos-cn-")
}

func optionalString(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func (s *Server) checkAuth(r *http.Request) error {
	if s.cfg.APIKey == "" {
		return nil
	}
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return errors.New("missing or invalid Authorization header")
	}
	token := strings.TrimPrefix(auth, "Bearer ")
	if token != s.cfg.APIKey {
		return errors.New("invalid API key")
	}
	return nil
}

func ratioToSize(ratio string) string {
	switch ratio {
	case "16:9":
		return "1792x1024"
	case "9:16":
		return "1024x1792"
	case "4:3":
		return "1024x768"
	case "3:4":
		return "768x1024"
	default:
		return "1024x1024"
	}
}

func sanitizeErrorMessage(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	return strings.TrimSpace(s)
}

func truncateForLog(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + fmt.Sprintf("...(%d more bytes)", len(s)-max)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		log.Printf("write json failed: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"code":    http.StatusText(status),
			"message": message,
		},
	})
}
