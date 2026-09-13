package server

import (
	"bytes"
	"image-gateway/internal/proxy"
	"image-gateway/internal/resolver"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type mockTransport func(*http.Request) (*http.Response, error)

func (f mockTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestRoutesAndMedia(t *testing.T) {
	mediaCalls := 0
	client := &http.Client{Transport: mockTransport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, ".json") {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":42,"file_url":"https://cdn.donmai.us/original.jpg","large_file_url":"https://cdn.donmai.us/sample.jpg","preview_file_url":"https://cdn.donmai.us/preview.jpg","file_ext":"jpg","source":"javascript:alert(1)","tag_string_general":"<script>"}`))}, nil
		}
		mediaCalls++
		if r.Header.Get("Referer") != "https://danbooru.donmai.us/" {
			t.Error("missing referer")
		}
		h := http.Header{"Content-Type": {"image/jpeg"}, "Content-Length": {"4"}, "ETag": {"test-tag"}}
		status := 200
		if r.Header.Get("Range") != "" {
			status = 206
			h.Set("Content-Range", "bytes 0-3/100")
		}
		return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader("JPEG"))}, nil
	})}
	reg := resolver.New(client, time.Minute, 10, "", "")
	router := New(reg, proxy.New(client, reg, 1))
	for _, path := range []string{"/", "/health", "/static/app.css", "/post/danbooru/42", "/view?url=https%3A%2F%2Fdanbooru.donmai.us%2Fposts%2F42", "/api/resolve?url=https%3A%2F%2Fdanbooru.donmai.us%2Fposts%2F42"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Header().Get("Content-Security-Policy"), "connect-src 'self'") {
			t.Fatal("CSP blocks same-origin API requests")
		}
		if strings.HasPrefix(path, "/post") {
			if strings.Contains(w.Body.String(), `src="/media/danbooru/42/original"`) || strings.Contains(w.Body.String(), `href="javascript:`) || strings.Contains(w.Body.String(), "<script>") {
				t.Fatal("unsafe rendering")
			}
			if !strings.Contains(w.Body.String(), `src="/media/danbooru/42/sample"`) || strings.Contains(w.Body.String(), `data-upgrade-src`) {
				t.Fatal("view does not use sample directly")
			}
		}
	}
	if mediaCalls != 0 {
		t.Fatal("view fetched media eagerly")
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/download/danbooru/42", nil))
	if w.Code != 200 || w.Body.String() != "JPEG" || !strings.Contains(w.Header().Get("Content-Disposition"), "danbooru_42.jpg") {
		t.Fatalf("download: %d %v", w.Code, w.Header())
	}
	req := httptest.NewRequest("GET", "/media/danbooru/42/original", nil)
	req.Header.Set("Range", "bytes=0-3")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 206 || w.Header().Get("Content-Range") != "bytes 0-3/100" {
		t.Fatal("range not forwarded")
	}
	for _, path := range []string{"/api/resolve?url=http://localhost", "/view?url=invalid", "/media/danbooru/42/evil", "/media/evil/42/original", "/post/danbooru/nope"} {
		w = httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 400 {
			t.Errorf("%s: %d", path, w.Code)
		}
		if strings.HasPrefix(path, "/api/") && !bytes.Contains(w.Body.Bytes(), []byte(`"error"`)) {
			t.Error("missing JSON error")
		}
	}
}
