package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port              string
	CDPURL            string
	CDPPort           int
	APIKey            string
	RequestTimeout    time.Duration
	VideoTimeout      time.Duration
	VideoUIMode       string // "skill" (default) or "office"
	AccountsDB        string
	ActiveSession     string
	ChromeScript      string
	AutoRestartChrome bool
	MaxParallelVideo  int // concurrent Seedance gens (one Chrome per worker)
	COSSecretID       string
	COSSecretKey      string
	COSBucket         string
	COSRegion         string
	COSPublicBaseURL  string
	COSAccelerate     bool
	COSKeyPrefix      string
}

func Load() Config {
	loadDotEnv(".env")

	timeoutSec := 120
	if v := os.Getenv("REQUEST_TIMEOUT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			timeoutSec = n
		}
	}

	// Seedance 2.0 Fast UI often estimates ~15 minutes; default 25m covers queue + URL extract.
	videoTimeoutSec := 1500
	if v := os.Getenv("VIDEO_TIMEOUT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			videoTimeoutSec = n
		}
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	cdpURL := os.Getenv("DOUBAO_CDP_URL")
	if cdpURL == "" {
		cdpURL = "http://127.0.0.1:9222"
	}

	cdpPort := 0
	if v := os.Getenv("DOUBAO_CDP_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cdpPort = n
		}
	}
	if cdpPort <= 0 {
		cdpPort = cdpPortFromURL(cdpURL)
	}

	videoUIMode := os.Getenv("VIDEO_UI_MODE")
	if videoUIMode == "" {
		videoUIMode = "skill"
	}

	accountsDB := os.Getenv("DOUBAO_ACCOUNTS_DB")
	if accountsDB == "" {
		accountsDB = "./data/accounts.db"
	}
	activeSession := os.Getenv("DOUBAO_ACTIVE_SESSION_FILE")
	if activeSession == "" {
		activeSession = "./data/active_session"
	}
	chromeScript := os.Getenv("DOUBAO_CHROME_SCRIPT")
	if chromeScript == "" {
		chromeScript = "./scripts/start-chrome.sh"
	}

	autoRestart := true
	if v := os.Getenv("DOUBAO_AUTO_RESTART_CHROME"); v != "" {
		autoRestart = v == "1" || stringsEqualFoldTrue(v)
	}

	maxParallel := 2
	if v := os.Getenv("MAX_PARALLEL_VIDEO"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxParallel = n
		}
	}

	cosPrefix := os.Getenv("COS_KEY_PREFIX")
	if cosPrefix == "" {
		cosPrefix = "doubao-web/videos"
	}

	return Config{
		Port:              port,
		CDPURL:            cdpURL,
		CDPPort:           cdpPort,
		APIKey:            os.Getenv("DOUBAO_API_KEY"),
		RequestTimeout:    time.Duration(timeoutSec) * time.Second,
		VideoTimeout:      time.Duration(videoTimeoutSec) * time.Second,
		VideoUIMode:       videoUIMode,
		AccountsDB:        accountsDB,
		ActiveSession:     activeSession,
		ChromeScript:      chromeScript,
		AutoRestartChrome: autoRestart,
		MaxParallelVideo:  maxParallel,
		COSSecretID:       "",
		COSSecretKey:      "",
		COSBucket:         envOr("COS_BUCKET", "novaly-1258504407"),
		COSRegion:         envOr("COS_REGION", "ap-shanghai"),
		COSPublicBaseURL:  strings.TrimSuffix(os.Getenv("COS_PUBLIC_BASE_URL"), "/"),
		COSAccelerate:     envTruthy("COS_ACCELERATE", false),
		COSKeyPrefix:      cosPrefix,
	}
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envTruthy(key string, fallback bool) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if v == "" {
		return fallback
	}
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func loadDotEnv(path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		i := strings.IndexByte(line, '=')
		if i <= 0 {
			continue
		}
		k := strings.TrimSpace(line[:i])
		v := strings.TrimSpace(line[i+1:])
		if len(v) >= 2 {
			if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
				v = v[1 : len(v)-1]
			}
		}
		if _, exists := os.LookupEnv(k); !exists {
			_ = os.Setenv(k, v)
		}
	}
}

func stringsEqualFoldTrue(v string) bool {
	switch v {
	case "true", "TRUE", "True", "yes", "YES", "on", "ON":
		return true
	default:
		return false
	}
}

func cdpPortFromURL(cdpURL string) int {
	if i := strings.LastIndex(cdpURL, ":"); i >= 0 {
		portStr := cdpURL[i+1:]
		if j := strings.IndexAny(portStr, "/?"); j >= 0 {
			portStr = portStr[:j]
		}
		if n, err := strconv.Atoi(portStr); err == nil && n > 0 {
			return n
		}
	}
	return 9222
}
