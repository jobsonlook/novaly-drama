package localservice

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestControlRejectsForeignOriginsAndHosts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	New().Register(r)
	for _, tc := range []struct {
		host, origin, header string
		want                 int
	}{
		{"127.0.0.1:8085", "https://evil.example", "1", 403},
		{"evil.example:8085", "", "1", 403},
		{"127.0.0.1:8085", "http://127.0.0.1:8085", "", 403},
		{"127.0.0.1:8085", "http://127.0.0.1:8085", "1", 200},
	} {
		req := httptest.NewRequest(http.MethodPost, "http://"+tc.host+"/api/local/doubao/stop", nil)
		req.Header.Set("Origin", tc.origin)
		req.Header.Set("X-Novaly-Local", tc.header)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Fatalf("%+v: got %d", tc, w.Code)
		}
	}
}
func TestMissingBinaryDoesNotStart(t *testing.T) {
	m := New()
	m.root = t.TempDir()
	if err := m.Start(); err == nil {
		t.Fatal("expected missing binary or occupied port error")
	}
	if m.cmd != nil {
		t.Fatal("unexpected child process")
	}
	if err := m.Stop(); err != nil {
		t.Fatal(err)
	}
}
