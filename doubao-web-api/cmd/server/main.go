package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mask/ai/doubao-web-api/internal/account"
	"github.com/mask/ai/doubao-web-api/internal/cdp"
	"github.com/mask/ai/doubao-web-api/internal/config"
	"github.com/mask/ai/doubao-web-api/internal/doubao"
	"github.com/mask/ai/doubao-web-api/internal/pool"
	"github.com/mask/ai/doubao-web-api/internal/server"
	"github.com/mask/ai/doubao-web-api/internal/storage"
)

func main() {
	cfg := config.Load()

	releaseLock, err := acquireProcessLock()
	if err != nil {
		log.Fatalf("%v", err)
	}
	defer releaseLock()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	accounts, err := account.Open(cfg.AccountsDB, cfg.ActiveSession)
	if err != nil {
		log.Fatalf("open accounts db: %v", err)
	}
	defer accounts.Close()
	accounts.StartDailyResetLoop(ctx)
	if active, err := accounts.Active(); err == nil {
		log.Printf("active account: id=%d name=%s fast_remaining=%d session_dir=%s",
			active.ID, active.Name, active.FastRemaining, active.SessionDir)
	} else {
		log.Printf("no active account yet — manage at http://127.0.0.1:%s/admin", cfg.Port)
	}

	log.Printf("starting chrome worker pool (max_parallel=%d base_port=%d video_ui_mode=%s)",
		cfg.MaxParallelVideo, cfg.CDPPort, cfg.VideoUIMode)
	workers, err := pool.Start(ctx, accounts, pool.Config{
		MaxParallel:    cfg.MaxParallelVideo,
		BaseCDPPort:    cfg.CDPPort,
		ChromeScript:   cfg.ChromeScript,
		VideoUIMode:    cfg.VideoUIMode,
		AutoStart:      cfg.AutoRestartChrome,
		FallbackCDPURL: cfg.CDPURL,
	})
	if err != nil {
		log.Fatalf("start worker pool: %v", err)
	}
	browser := workers.DefaultBrowser()
	if browser == nil {
		log.Fatalf("worker pool has no default browser")
	}
	chromeMgr := workers.DefaultManager()

	client := doubao.NewClient(
		browser.FetchSamantha,
		browser.FetchSamanthaAsyncStream,
		func(ctx context.Context, data []byte, filename string) (doubao.UploadResult, error) {
			result, err := browser.UploadMedia(ctx, data, filename)
			if err != nil {
				return doubao.UploadResult{}, err
			}
			return doubao.UploadResult{
				URI:    result.URI,
				URL:    result.URL,
				Name:   result.Name,
				Format: result.Format,
			}, nil
		},
		browser.GetConversationID,
		func(ctx context.Context, opts doubao.GenerateVideoOptions) ([]doubao.VideoResult, error) {
			// Fallback path when Server has no pool; prefer pool.Acquire in server.
			imageFiles := make([]cdp.LocalMediaFile, 0, len(opts.RefImageFiles))
			for _, f := range opts.RefImageFiles {
				imageFiles = append(imageFiles, cdp.LocalMediaFile{Data: f.Data, Filename: f.Filename})
			}
			items, err := browser.GenerateVideoViaUI(ctx, cdp.VideoUIOptions{
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
			})
			if err != nil {
				return nil, err
			}
			return cdpVideoItemsToResults(items), nil
		},
		func(ctx context.Context, conversationID string, timeout time.Duration) ([]doubao.VideoResult, error) {
			items, err := browser.WaitForVideos(ctx, conversationID, timeout)
			if err != nil {
				return nil, err
			}
			return cdpVideoItemsToResults(items), nil
		},
	)

	cosClient, err := storage.NewCOS(storage.COSConfig{
		SecretID:      cfg.COSSecretID,
		SecretKey:     cfg.COSSecretKey,
		Bucket:        cfg.COSBucket,
		Region:        cfg.COSRegion,
		PublicBaseURL: cfg.COSPublicBaseURL,
		Accelerate:    cfg.COSAccelerate,
		KeyPrefix:     cfg.COSKeyPrefix,
	})
	if err != nil {
		log.Fatalf("init COS: %v", err)
	}
	if cosClient != nil {
		log.Printf("COS upload enabled (bucket=%s region=%s accelerate=%v)", cfg.COSBucket, cfg.COSRegion, cfg.COSAccelerate)
	} else {
		log.Printf("COS upload disabled (set COS_SECRET_ID / COS_SECRET_KEY to enable)")
	}

	srv := server.New(cfg, client, accounts, chromeMgr, cosClient)
	srv.SetPool(workers)

	httpServer := &http.Server{
		Addr:    "127.0.0.1:" + cfg.Port,
		Handler: srv.Handler(),
		// ReadHeaderTimeout bounds slowloris; ReadTimeout must cover large
		// multipart uploads (Novaly ref images can be multi‑MB each).
		ReadHeaderTimeout: 15 * time.Second,
		ReadTimeout:       10 * time.Minute,
		WriteTimeout:      cfg.RequestTimeout + 30*time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		log.Printf("doubao-web-api listening on http://127.0.0.1:%s", cfg.Port)
		log.Printf("admin:     GET  http://127.0.0.1:%s/admin", cfg.Port)
		log.Printf("endpoint: POST http://127.0.0.1:%s/api/v3/images/generations", cfg.Port)
		log.Printf("endpoint: POST http://127.0.0.1:%s/api/v3/images/uploads", cfg.Port)
		log.Printf("endpoint: POST http://127.0.0.1:%s/api/v3/contents/generations/tasks", cfg.Port)
		log.Printf("endpoint: GET  http://127.0.0.1:%s/api/v3/contents/generations/tasks/{id}", cfg.Port)
		for _, w := range workers.Workers() {
			log.Printf("worker: account=%s id=%d port=%d remaining=%d busy=%v",
				w.Name, w.AccountID, w.CDPPort, w.Remaining, w.Busy)
		}
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Printf("shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
	if err := workers.Stop(shutdownCtx); err != nil {
		log.Printf("worker pool stop: %v", err)
	}
}

func cdpVideoItemsToResults(items []cdp.VideoItem) []doubao.VideoResult {
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
	return out
}

func acquireProcessLock() (func(), error) {
	lockPath := filepath.Join(os.TempDir(), "doubao-web-api-cdp.lock")
	if err := removeStaleProcessLock(lockPath); err != nil {
		return nil, err
	}

	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if os.IsExist(err) {
			pid := readLockPID(lockPath)
			return nil, fmt.Errorf("another doubao-web-api server is already running (pid=%d, lock: %s); stop it before starting again", pid, lockPath)
		}
		return nil, err
	}
	_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
	_ = f.Close()
	return func() { _ = os.Remove(lockPath) }, nil
}

func removeStaleProcessLock(lockPath string) error {
	data, err := os.ReadFile(lockPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 || !processAlive(pid) {
		log.Printf("removing stale server lock (pid=%d)", pid)
		return os.Remove(lockPath)
	}
	return nil
}

func readLockPID(lockPath string) int {
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return pid
}

func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}
