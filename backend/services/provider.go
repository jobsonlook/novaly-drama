package services

import (
	"errors"
	"net/url"
	"strings"

	"novaly/backend/models"
)

const DoubaoWebAPISlug = "doubao-web-api"
const PixAPISlug = "pixapi"
const XaisSlug = "xais"
const DeepSeekSlug = "deepseek"

func IsDoubaoWebAPI(provider models.AIProvider) bool {
	return provider.Slug == DoubaoWebAPISlug
}

func IsPixAPI(provider models.AIProvider) bool {
	return provider.Slug == PixAPISlug
}

func IsXais(provider models.AIProvider) bool {
	return provider.Slug == XaisSlug
}

func IsDeepSeek(provider models.AIProvider) bool {
	return provider.Slug == DeepSeekSlug
}

func ProviderRequiresAPIKey(provider models.AIProvider) bool {
	return !IsDoubaoWebAPI(provider)
}

func PixAPIReferenceURLError(publicBaseURL string, tos *TOSStorage, cosEnabled bool, refRelayOK bool) error {
	if refRelayOK || cosEnabled {
		return nil
	}
	if CanUseLocalPixAPIRef(publicBaseURL) {
		return nil
	}
	if tos != nil && tos.Enabled() {
		return nil
	}
	if publicBaseURL == "" {
		return errors.New("PixAPI 图生图需要公网可访问的参考图地址，请配置东京中继 PIXAPI_BASE_URL、COS、PUBLIC_BASE_URL 或 TOS")
	}
	return errors.New("PUBLIC_BASE_URL 为本地/内网地址，PixAPI 服务器无法拉取参考图，请使用东京中继、公网 IP/域名、COS 或 TOS")
}

// CanUseLocalPixAPIRef reports whether PUBLIC_BASE_URL can be used so PixAPI
// fetches reference images from this server (no TOS upload).
func CanUseLocalPixAPIRef(publicBaseURL string) bool {
	if publicBaseURL == "" {
		return false
	}
	host := publicBaseURL
	if parsed, err := url.Parse(publicBaseURL); err == nil && parsed.Host != "" {
		host = parsed.Host
	}
	host = strings.ToLower(strings.Split(host, ":")[0])
	if host == "localhost" || host == "127.0.0.1" || strings.HasPrefix(host, "192.168.") || strings.HasPrefix(host, "10.") {
		return false
	}
	return true
}
