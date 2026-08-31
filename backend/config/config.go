package config

import (
	"os"
	"strings"
)

type Config struct {
	Port, DatabasePath, ArkAPIKey, ArkModel, ArkBaseURL, DoubaoWebBaseURL, DoubaoWebAPIKey, PixAPIAPIKey, PixAPIBaseURL, PixAPIHTTPProxy, PublicBaseURL string
	XaisAPIKey, XaisBaseURL, DeepSeekAPIKey, DeepSeekBaseURL                                                                                            string
	AccessToken                                                                                                                                         string
	TOSAccessKeyID, TOSSecretAccessKey, TOSBucket, TOSRegion, TOSEndpoint                                                                               string
	COSSecretID, COSSecretKey, COSBucket, COSRegion, COSPublicBaseURL                                                                                   string
	COSAccelerate                                                                                                                                       bool
	VolcTTSAppID, VolcTTSAccessToken, VolcTTSAPIKey, VolcTTSCluster                                                                                     string
	LogPath                                                                                                                                             string
}

func Load() Config {
	loadDotEnv("../.env")
	return Config{
		Port:               env("PORT", "8085"),
		DatabasePath:       env("DATABASE_PATH", "data/novaly.db"),
		ArkAPIKey:          os.Getenv("ARK_API_KEY"),
		ArkModel:           os.Getenv("ARK_MODEL"),
		ArkBaseURL:         strings.TrimSuffix(env("ARK_BASE_URL", "https://ark.cn-beijing.volces.com/api/v3"), "/"),
		DoubaoWebBaseURL:   normalizeDoubaoWebBaseURL(env("DOUBAO_WEB_API_URL", "http://127.0.0.1:8086/api/v3")),
		DoubaoWebAPIKey:    strings.TrimSpace(os.Getenv("DOUBAO_WEB_API_KEY")),
		PixAPIAPIKey:       os.Getenv("PIXAPI_API_KEY"),
		PixAPIBaseURL:      strings.TrimSuffix(env("PIXAPI_BASE_URL", "https://api.pixapi.ai/v1"), "/"),
		PixAPIHTTPProxy:    strings.TrimSpace(os.Getenv("PIXAPI_HTTP_PROXY")),
		XaisAPIKey:         strings.TrimSpace(os.Getenv("XAIS_API_KEY")),
		XaisBaseURL:        strings.TrimSuffix(env("XAIS_BASE_URL", "https://sg2.dchai.cn"), "/"),
		DeepSeekAPIKey:     strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY")),
		DeepSeekBaseURL:    strings.TrimSuffix(env("DEEPSEEK_BASE_URL", "https://api.deepseek.com/v1"), "/"),
		PublicBaseURL:      strings.TrimSuffix(os.Getenv("PUBLIC_BASE_URL"), "/"),
		AccessToken:        strings.TrimSpace(os.Getenv("ACCESS_TOKEN")),
		TOSAccessKeyID:     os.Getenv("TOS_ACCESS_KEY_ID"),
		TOSSecretAccessKey: os.Getenv("TOS_SECRET_ACCESS_KEY"),
		TOSBucket:          env("TOS_BUCKET", "novaly"),
		TOSRegion:          env("TOS_REGION", "cn-shanghai"),
		TOSEndpoint:        env("TOS_ENDPOINT", "https://tos-cn-shanghai.volces.com"),
		COSSecretID:        os.Getenv("COS_SECRET_ID"),
		COSSecretKey:       os.Getenv("COS_SECRET_KEY"),
		COSBucket:          env("COS_BUCKET", "novaly-1258504407"),
		COSRegion:          env("COS_REGION", "ap-shanghai"),
		COSPublicBaseURL:   strings.TrimSuffix(os.Getenv("COS_PUBLIC_BASE_URL"), "/"),
		COSAccelerate:      envTruthy("COS_ACCELERATE", false),
		VolcTTSAppID:       strings.TrimSpace(os.Getenv("VOLC_TTS_APP_ID")),
		VolcTTSAccessToken: strings.TrimSpace(os.Getenv("VOLC_TTS_ACCESS_TOKEN")),
		VolcTTSAPIKey:      strings.TrimSpace(os.Getenv("VOLC_TTS_API_KEY")),
		VolcTTSCluster:     env("VOLC_TTS_CLUSTER", "volcano_tts"),
		LogPath:            env("NOVALY_LOG_PATH", "../logs/novaly.log"),
	}
}

// normalizeDoubaoWebBaseURL accepts either http://host:8080 or .../api/v3.
func normalizeDoubaoWebBaseURL(raw string) string {
	base := strings.TrimSuffix(strings.TrimSpace(raw), "/")
	if base == "" {
		return "http://127.0.0.1:8086/api/v3"
	}
	if !strings.HasSuffix(base, "/api/v3") {
		base += "/api/v3"
	}
	return base
}
func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
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
		key, value, ok := strings.Cut(line, "=")
		if !ok || os.Getenv(strings.TrimSpace(key)) != "" {
			continue
		}
		_ = os.Setenv(strings.TrimSpace(key), strings.Trim(strings.TrimSpace(value), "\"'"))
	}
}
