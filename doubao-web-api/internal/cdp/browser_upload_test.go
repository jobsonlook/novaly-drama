package cdp

import "testing"

func TestSniffImageFormatPNG(t *testing.T) {
	data := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	ext, mime := sniffImageFormat(data)
	if ext != "png" || mime != "image/png" {
		t.Fatalf("got ext=%q mime=%q", ext, mime)
	}
}

func TestSniffImageFormatJPEG(t *testing.T) {
	data := []byte{0xFF, 0xD8, 0xFF, 0xE0}
	ext, mime := sniffImageFormat(data)
	if ext != "jpg" || mime != "image/jpeg" {
		t.Fatalf("got ext=%q mime=%q", ext, mime)
	}
}

func TestImageUploadMetaMismatchedExtension(t *testing.T) {
	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00}
	meta := resolveUploadImageMeta(pngHeader, "场景1的背视图.jpg")
	if meta.Ext != "png" {
		t.Fatalf("ext = %q, want png", meta.Ext)
	}
	if meta.MIME != "image/png" {
		t.Fatalf("mime = %q", meta.MIME)
	}
	if meta.UploadName != "upload.png" {
		t.Fatalf("uploadName = %q", meta.UploadName)
	}
}

func TestImageMIMEJPEG(t *testing.T) {
	if imageMIMEForUpload("jpg") != "image/jpeg" {
		t.Fatal("jpg mime should be image/jpeg")
	}
}

func TestIsDoubaoChatURL(t *testing.T) {
	if !isDoubaoChatURL("https://www.doubao.com/chat/") {
		t.Fatal("expected doubao chat url")
	}
	if isDoubaoChatURL("chrome://newtab/") {
		t.Fatal("new tab should not match")
	}
}

func TestIsBlankBrowserURL(t *testing.T) {
	cases := []string{"", "about:blank", "chrome://newtab/", "chrome://new-tab-page/"}
	for _, u := range cases {
		if !isBlankBrowserURL(u) {
			t.Fatalf("%q should be blank", u)
		}
	}
	if isBlankBrowserURL("https://www.doubao.com/chat/") {
		t.Fatal("doubao chat is not blank")
	}
}
