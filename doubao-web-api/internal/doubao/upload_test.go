package doubao

import (
	"encoding/base64"
	"testing"
)

func TestDecodeDataURI(t *testing.T) {
	data, name, err := decodeDataURI("data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("png-bytes")))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "png-bytes" {
		t.Fatalf("data = %q", data)
	}
	if name != "image.png" {
		t.Fatalf("name = %q", name)
	}
}

func TestExtractRefImageKeyURI(t *testing.T) {
	c := NewClient(nil, nil, nil, nil, nil, nil)
	key, err := c.ExtractRefImageKey(t.Context(), "tos-cn-i-xxx/image/key")
	if err != nil {
		t.Fatal(err)
	}
	if key != "tos-cn-i-xxx/image/key" {
		t.Fatalf("key = %q", key)
	}
}

func TestExtractRefImageKeyURLRejected(t *testing.T) {
	c := NewClient(nil, nil, nil, nil, nil, nil)
	_, err := c.ExtractRefImageKey(t.Context(), "https://example.com/a.png")
	if err == nil {
		t.Fatal("expected error for http url")
	}
}
