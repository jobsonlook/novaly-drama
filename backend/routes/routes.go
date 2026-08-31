package routes

import (
	"crypto/subtle"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"novaly/backend/config"
	"novaly/backend/controllers"
	"novaly/backend/database"
	"novaly/backend/services"
)

const accessTokenCookie = "novaly_token"

func New(db *gorm.DB, cfg config.Config) *gin.Engine {
	if err := database.SeedArk(db, cfg); err != nil {
		panic(err)
	}
	cosStorage, err := services.NewCOSStorage(services.COSConfig{})
	if err != nil {
		panic(err)
	}
	storage := services.NewStorage("data/uploads", cosStorage)
	if err := database.BackfillVideoResources(db, storage); err != nil {
		panic(err)
	}
	if err := database.BackfillTransitionFramePaths(db, storage); err != nil {
		panic(err)
	}
	if err := database.BackfillTransitionFrameRefLabels(db); err != nil {
		panic(err)
	}
	if err := database.BackfillShotScriptArtifacts(db); err != nil {
		panic(err)
	}
	if err := database.BackfillShotScriptRepetition(db); err != nil {
		panic(err)
	}
	ark := services.NewArkService(cfg.PixAPIHTTPProxy, services.DerivePixAPIAssetRelay(cfg.PixAPIBaseURL, os.Getenv("PIXAPI_ASSET_RELAY")))
	tosStorage, err := services.NewTOSStorage(services.TOSConfig{
		AccessKeyID:     "",
		SecretAccessKey: "",
		Bucket:          cfg.TOSBucket,
		Region:          cfg.TOSRegion,
		Endpoint:        cfg.TOSEndpoint,
	})
	if err != nil {
		panic(err)
	}
	r := gin.New()
	r.MaxMultipartMemory = 512 << 20
	r.Use(gin.Logger(), gin.Recovery(), cors(), accessGate(cfg.AccessToken))
	project := &controllers.ProjectController{DB: db, Storage: storage}
	episode := &controllers.EpisodeController{DB: db, Storage: storage}
	resource := &controllers.ResourceController{
		DB: db, Ark: ark, Storage: storage, TOS: tosStorage, PublicBaseURL: cfg.PublicBaseURL,
		PixRefRelay: services.DerivePixAPIRelayOrigin(cfg.PixAPIBaseURL),
	}
	shot := &controllers.ShotController{DB: db, Ark: ark, Storage: storage, Resource: resource}
	crewCtl := &controllers.CrewController{DB: db, Ark: ark, Storage: storage, Resource: resource}
	editor := &controllers.EditorController{DB: db, Storage: storage}
	shot.ResumeInterruptedVideoJobs()
	direct := &controllers.DirectUploadController{DB: db, Storage: storage}
	settings := &controllers.SettingsController{DB: db, Ark: ark}
	tts := &controllers.TTSController{
		DB:  db,
		Ark: ark,
		TTS: services.NewVolcTTSService(services.VolcTTSConfig{
			AppID:       cfg.VolcTTSAppID,
			AccessToken: cfg.VolcTTSAccessToken,
			APIKey:      cfg.VolcTTSAPIKey,
			Cluster:     cfg.VolcTTSCluster,
		}),
		DataDir: "data/tts",
	}
	api := r.Group("/api")
	api.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })
	logs := &controllers.LogsController{LogPath: cfg.LogPath}
	api.GET("/logs", logs.Tail)
	api.GET("/logs/download", logs.Download)
	api.GET("/cos/status", direct.Status)
	api.POST("/cos/multipart/init", direct.InitMultipart)
	api.POST("/cos/multipart/sign-parts", direct.SignMultipartParts)
	api.POST("/cos/multipart/complete", direct.CompleteMultipart)
	api.POST("/cos/multipart/abort", direct.AbortMultipart)
	api.GET("/uploads/*filepath", func(c *gin.Context) {
		rel := strings.TrimPrefix(c.Param("filepath"), "/")
		localPath := filepath.Join("data/uploads", rel)
		isVideo := strings.HasSuffix(strings.ToLower(rel), ".mp4") ||
			strings.HasSuffix(strings.ToLower(rel), ".webm") ||
			strings.HasSuffix(strings.ToLower(rel), ".mov") ||
			strings.HasSuffix(strings.ToLower(rel), ".m4v")
		// Videos always redirect to COS when configured — never serve stale local copies.
		if isVideo && storage.COSEnabled() {
			c.Redirect(http.StatusFound, storage.COS.PublicURL(rel))
			return
		}
		// Prefer local file when present so generate/upload isn't blocked on COS sync.
		if st, err := os.Stat(localPath); err == nil && !st.IsDir() {
			if strings.Contains(rel, "_stylized.") {
				c.Header("Cache-Control", "no-cache, must-revalidate")
			}
			c.File(localPath)
			return
		}
		if storage.COSEnabled() {
			target := storage.COS.PublicURL(rel)
			if strings.Contains(rel, "_stylized.") {
				sep := "?"
				if strings.Contains(target, "?") {
					sep = "&"
				}
				target = target + sep + "v=" + time.Now().Format("150405")
			}
			c.Redirect(http.StatusFound, target)
			return
		}
		c.Status(http.StatusNotFound)
	})
	api.GET("/projects/trash", project.ListTrash)
	api.GET("/projects", project.List)
	api.POST("/projects", project.Create)
	api.GET("/projects/:id", project.Get)
	api.PUT("/projects/:id", project.Update)
	api.DELETE("/projects/:id", project.Delete)
	api.POST("/projects/:id/restore", project.Restore)
	api.DELETE("/projects/:id/permanent", project.Purge)
	api.POST("/projects/:id/episodes", episode.Add)
	api.GET("/projects/:id/resources", resource.List)
	api.GET("/projects/:id/resources/export-videos", resource.ExportVideos)
	api.GET("/projects/:id/resources/trash", resource.ListTrash)
	api.POST("/projects/:id/resources/generate-character", resource.GenerateCharacter)
	api.POST("/projects/:id/resources/generate-scene", resource.GenerateScene)
	api.POST("/projects/:id/resources/generate-prop", resource.GenerateProp)
	api.POST("/projects/:id/resources/batch-prompts", resource.BatchPrompts)
	api.POST("/projects/:id/resources/batch-images", resource.BatchImages)
	api.POST("/projects/:id/resources/generate-scene-grid", resource.GenerateSceneGrid)
	api.POST("/projects/:id/resources/analyze-scene-grid-legend", resource.AnalyzeSceneGridShapeLegend)
	api.POST("/projects/:id/resources/generate-scene-reverse", resource.GenerateSceneReverse)
	api.POST("/projects/:id/resources/generate-scene-panorama", resource.GenerateScenePanorama)
	api.GET("/projects/:id/resources/generate-jobs", resource.ListImageGenerationJobs)
	api.POST("/projects/:id/resources/generate-jobs/:jobId/dismiss", resource.DismissImageGenerationJob)
	api.GET("/projects/:id/resources/generate-jobs/:jobId", resource.GetImageGenerationJob)
	api.POST("/projects/:id/resources", resource.Create)
	api.POST("/projects/:id/resources/upload-videos", resource.UploadVideos)
	api.POST("/projects/:id/resources/direct-upload", direct.PresignResourceImage)
	api.POST("/projects/:id/resources/direct-upload-videos", direct.PresignResourceVideos)
	api.POST("/resources/:id/confirm-image", direct.ConfirmResourceImage)
	api.POST("/resources/:id/confirm-video", direct.ConfirmResourceVideo)
	api.POST("/resources/:id/stylize", resource.Stylize)
	api.POST("/resources/:id/split-grid", resource.SplitGrid)
	api.POST("/resources/:id/split-panorama", resource.SplitPanorama)
	api.GET("/resources/:id/grid-cells", resource.GridCells)
	api.POST("/resources/:id/use-primary", resource.UsePrimary)
	api.PUT("/resources/:id", resource.Update)
	api.GET("/resources/:id/download", resource.Download)
	api.DELETE("/resources/:id", resource.Delete)
	api.DELETE("/resources/:id/permanent", resource.Purge)
	api.GET("/episodes/:id", episode.Get)
	api.GET("/episodes/:id/editor", editor.Load)
	api.PUT("/episodes/:id/editor", editor.Save)
	api.PUT("/episodes/:id", episode.Update)
	api.POST("/projects/:id/crew/extract", crewCtl.Extract)
	api.GET("/episodes/:id/crew", crewCtl.Get)
	api.POST("/episodes/:id/crew/chat", crewCtl.Chat)
	api.DELETE("/episodes/:id/crew/memory", crewCtl.ClearMemory)
	api.POST("/episodes/:id/crew/start", crewCtl.Start)
	api.POST("/episodes/:id/crew/continue", crewCtl.Continue)
	api.POST("/episodes/:id/crew/resplit-from", crewCtl.ResplitFrom)
	api.POST("/episodes/:id/crew/retry", crewCtl.Retry)
	api.POST("/episodes/:id/crew/rewind", crewCtl.Rewind)
	api.POST("/episodes/:id/crew/fix", crewCtl.Fix)
	api.DELETE("/episodes/:id", episode.Delete)
	api.POST("/episodes/:id/shots", episode.AddShot)
	api.PUT("/episodes/:id/shots/reorder", episode.ReorderShots)
	api.GET("/shots/:id", shot.Get)
	api.PUT("/shots/:id/move", shot.Move)
	api.PUT("/shots/:id", shot.Update)
	api.DELETE("/shots/:id", shot.Delete)
	api.GET("/shots/:id/prompt-preview", shot.PreviewPrompt)
	api.POST("/shots/:id/analyze-positioning", shot.AnalyzePositioning)
	api.POST("/shots/:id/generate-positioning", shot.GeneratePositioning)
	api.POST("/shots/:id/analyze-motion-grid", shot.AnalyzeMotionGrid)
	api.POST("/shots/:id/generate-motion-grid", shot.GenerateMotionGrid)
	api.POST("/shots/:id/optimize-script", shot.OptimizeScript)
	api.POST("/shots/:id/match-refs", shot.MatchRefs)
	api.POST("/shots/:id/previous-frame", shot.PreviousFrame)
	api.POST("/shots/:id/generate", shot.Generate)
	api.POST("/shots/:id/upload-video", shot.UploadVideo)
	api.POST("/shots/:id/direct-upload-video", direct.PresignShotVideo)
	api.POST("/shots/:id/confirm-video", direct.ConfirmShotVideo)
	api.POST("/shots/:id/use-video", shot.UseVideo)
	api.GET("/shots/:id/download", shot.Download)
	api.GET("/settings/providers", settings.ListProviders)
	api.GET("/settings/providers/:id/api-key", settings.RevealAPIKey)
	api.PUT("/settings/providers/:id", settings.UpdateProvider)
	api.POST("/settings/providers/:id/models", settings.AddModel)
	api.PUT("/settings/models/:id", settings.UpdateModel)
	api.POST("/settings/providers/:id/test", settings.TestProvider)
	api.GET("/tts/status", tts.Status)
	api.GET("/tts/from-project/:projectId/shots", tts.ListExtractShots)
	api.POST("/tts/from-project/:projectId", tts.ExtractProject)
	api.POST("/tts/synthesize", tts.Synthesize)
	api.POST("/tts/batch", tts.Batch)
	api.GET("/tts/jobs/:jobId", tts.GetJob)
	api.GET("/tts/download/:jobId", tts.Download)
	api.GET("/tts/files/*filepath", tts.ServeFile)
	api.GET("/tts/projects", tts.ListProjects)
	api.POST("/tts/projects", tts.SaveProject)
	api.GET("/tts/projects/:id", tts.GetProject)
	api.PUT("/tts/projects/:id", tts.SaveProject)
	api.DELETE("/tts/projects/:id", tts.DeleteProject)
	api.POST("/tts/projects/:id/synthesize", tts.ProjectSynthesize)
	api.POST("/tts/projects/:id/batch", tts.ProjectBatch)
	api.GET("/tts/projects/:id/download", tts.DownloadProjectZip)
	mountFrontend(r)
	return r
}

func mountFrontend(r *gin.Engine) {
	candidates := []string{
		filepath.Clean("../frontend/dist"),
		filepath.Clean("frontend/dist"),
		filepath.Clean("dist"),
	}
	var dist string
	for _, p := range candidates {
		if st, err := os.Stat(filepath.Join(p, "index.html")); err == nil && !st.IsDir() {
			dist = p
			break
		}
	}

	deskCandidates := []string{
		filepath.Clean("../frontend/director-desk"),
		filepath.Clean("frontend/director-desk"),
		filepath.Clean("director-desk"),
	}
	var deskDist string
	for _, p := range deskCandidates {
		if st, err := os.Stat(filepath.Join(p, "index.html")); err == nil && !st.IsDir() {
			deskDist = p
			break
		}
	}
	if deskDist != "" {
		r.Static("/director-desk", deskDist)
		// SPA entry when opening /director-desk without trailing slash assets.
		r.GET("/director-desk", func(c *gin.Context) {
			c.File(filepath.Join(deskDist, "index.html"))
		})
	}

	if dist == "" {
		return
	}
	assets := filepath.Join(dist, "assets")
	if st, err := os.Stat(assets); err == nil && st.IsDir() {
		r.Static("/assets", assets)
	}
	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/api") {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		if strings.HasPrefix(path, "/director-desk") {
			if deskDist != "" {
				c.File(filepath.Join(deskDist, "index.html"))
				return
			}
			c.JSON(http.StatusNotFound, gin.H{"error": "director-desk not built"})
			return
		}
		rel := strings.TrimPrefix(path, "/")
		if rel != "" {
			file := filepath.Join(dist, rel)
			if st, err := os.Stat(file); err == nil && !st.IsDir() {
				c.File(file)
				return
			}
		}
		c.File(filepath.Join(dist, "index.html"))
	})
}

func accessGate(expected string) gin.HandlerFunc {
	expected = strings.TrimSpace(expected)
	return func(c *gin.Context) {
		if expected == "" || c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}
		// Keep health probe usable for deploy checks.
		if c.Request.URL.Path == "/api/health" {
			c.Next()
			return
		}
		// PixAPI (and browsers) must fetch reference images without a session cookie.
		if (c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead) &&
			strings.HasPrefix(c.Request.URL.Path, "/api/uploads/") {
			c.Next()
			return
		}

		queryToken := strings.TrimSpace(c.Query("token"))
		if queryToken != "" {
			if tokenMatch(queryToken, expected) {
				setAccessCookie(c, expected)
				// Strip token from URL after first successful visit.
				u := *c.Request.URL
				q := u.Query()
				q.Del("token")
				u.RawQuery = q.Encode()
				target := u.RequestURI()
				if target == "" {
					target = "/"
				}
				c.Redirect(http.StatusFound, target)
				c.Abort()
				return
			}
			rejectAccess(c)
			return
		}

		if tokenMatch(readAccessToken(c), expected) {
			c.Next()
			return
		}
		rejectAccess(c)
	}
}

func readAccessToken(c *gin.Context) string {
	if v, err := c.Cookie(accessTokenCookie); err == nil {
		if t := strings.TrimSpace(v); t != "" {
			return t
		}
	}
	if t := strings.TrimSpace(c.GetHeader("X-Access-Token")); t != "" {
		return t
	}
	auth := strings.TrimSpace(c.GetHeader("Authorization"))
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	return ""
}

func setAccessCookie(c *gin.Context, token string) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     accessTokenCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int((30 * 24 * time.Hour).Seconds()),
	})
}

func tokenMatch(got, expected string) bool {
	got = strings.TrimSpace(got)
	if unescaped, err := url.QueryUnescape(got); err == nil && unescaped != "" {
		got = strings.TrimSpace(unescaped)
	}
	if got == "" || expected == "" {
		return false
	}
	if len(got) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1
}

func rejectAccess(c *gin.Context) {
	if strings.HasPrefix(c.Request.URL.Path, "/api") {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "需要访问令牌，请使用 /?token=xxx 打开"})
		return
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(http.StatusUnauthorized)
	_, _ = c.Writer.Write([]byte(`<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><title>需要访问令牌</title>
<style>
body{margin:0;min-height:100vh;display:grid;place-items:center;background:#141210;color:#eee9e1;font-family:system-ui,sans-serif}
main{max-width:420px;padding:32px;border:1px solid #3c3731;border-radius:16px;background:#1f1c19}
h1{margin:0 0 12px;font-size:22px}p{margin:0;color:#b9afa5;line-height:1.6}
code{color:#ff785a}
</style></head><body><main>
<h1>需要访问令牌</h1>
<p>请使用带 token 的地址打开，例如：<br><code>/?token=你的令牌</code></p>
</main></body></html>`))
	c.Abort()
}

func cors() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin == "" {
			origin = "*"
		}
		c.Header("Access-Control-Allow-Origin", origin)
		c.Header("Access-Control-Allow-Headers", "Content-Type, X-Access-Token, Authorization")
		c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		c.Header("Access-Control-Allow-Credentials", "true")
		c.Header("Vary", "Origin")
		if c.Request.Method == http.MethodOptions {
			c.Status(204)
			c.Abort()
			return
		}
		c.Next()
	}
}
