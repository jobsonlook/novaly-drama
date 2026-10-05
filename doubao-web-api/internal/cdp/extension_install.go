package cdp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/chromedp/cdproto/extensions"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/mask/ai/doubao-web-api/internal/chrome"
)

func (b *Browser) installUnwatermarkExtension() {
	if b == nil || b.browserCtx == nil {
		return
	}
	dir := chrome.ResolveExtensionDir("")
	if dir == "" {
		log.Printf("cdp: unwatermark extension not found (set DOUBAO_CHROME_EXTENSION_DIR)")
		return
	}
	if id, err := loadUnpackedExtension(b.browserCtx, dir); err != nil {
		log.Printf("cdp: Extensions.loadUnpacked skipped: %v", err)
	} else if id != "" {
		log.Printf("cdp: loaded unpacked extension id=%s from %s", id, dir)
	}
	src, err := buildUnwatermarkInstallScript(dir)
	if err != nil {
		log.Printf("cdp: build unwatermark script: %v", err)
		return
	}
	if err := injectUnwatermarkScript(b.browserCtx, src); err != nil {
		log.Printf("cdp: inject unwatermark button: %v", err)
		return
	}
	log.Printf("cdp: unwatermark download button installed from %s", dir)
}

func loadUnpackedExtension(tabCtx context.Context, dir string) (string, error) {
	var id string
	err := chromedp.Run(tabCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		id, err = extensions.LoadUnpacked(dir).Do(ctx)
		return err
	}))
	return id, err
}

func injectUnwatermarkScript(tabCtx context.Context, src string) error {
	return chromedp.Run(tabCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := page.AddScriptToEvaluateOnNewDocument(src).WithRunImmediately(true).Do(ctx)
		return err
	}))
}

func buildUnwatermarkInstallScript(dir string) (string, error) {
	css, err := os.ReadFile(filepath.Join(dir, "content", "panel.css"))
	if err != nil {
		return "", err
	}
	injectJS, err := os.ReadFile(filepath.Join(dir, "content", "inject.js"))
	if err != nil {
		return "", err
	}
	unwatermarkJS, err := os.ReadFile(filepath.Join(dir, "content", "unwatermark.js"))
	if err != nil {
		return "", err
	}
	contentJS, err := os.ReadFile(filepath.Join(dir, "content", "content.js"))
	if err != nil {
		return "", err
	}
	cssJSON, err := json.Marshal(string(css))
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("(() => { try {")
	b.WriteString("if (document.documentElement && !document.getElementById('doubao-clean-dl-style')) {")
	b.WriteString("const st = document.createElement('style');")
	b.WriteString("st.id = 'doubao-clean-dl-style';")
	fmt.Fprintf(&b, "st.textContent = %s;", cssJSON)
	b.WriteString("(document.head || document.documentElement).appendChild(st);")
	b.WriteString("}} catch (e) {} })();\n")
	b.Write(injectJS)
	b.WriteByte('\n')
	b.Write(unwatermarkJS)
	b.WriteByte('\n')
	b.Write(contentJS)
	return b.String(), nil
}
