package proxy

import (
	"image-gateway/internal/resolver"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeTransport func(*http.Request) (*http.Response, error)

func (f fakeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type stream struct {
	writer    *httptest.ResponseRecorder
	remaining int
	closed    bool
	t         *testing.T
}

func (s *stream) Read(p []byte) (int, error) {
	if s.remaining == 0 {
		return 0, io.EOF
	}
	if s.remaining < 1024*1024 && s.writer.Body.Len() == 0 {
		s.t.Error("body buffered instead of streamed")
	}
	n := len(p)
	if n > s.remaining {
		n = s.remaining
	}
	clear(p[:n])
	s.remaining -= n
	return n, nil
}
func (s *stream) Close() error { s.closed = true; return nil }
func TestStreamingAndConcurrency(t *testing.T) {
	w := httptest.NewRecorder()
	body := &stream{writer: w, remaining: 1024 * 1024, t: t}
	c := &http.Client{Transport: fakeTransport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, ".json") {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"id":1,"file_url":"https://cdn.donmai.us/a.jpg"}`))}, nil
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/jpeg"}}, Body: body}, nil
	})}
	p := New(c, resolver.New(c, time.Minute, 2, "", ""), 1)
	req := httptest.NewRequest("GET", "/", nil)
	status, err := p.Serve(w, req, "danbooru", "1", "original", false)
	if status != 200 || err != nil || !body.closed || w.Body.Len() != 1024*1024 {
		t.Fatalf("stream: %d %v", status, err)
	}
	p.slots <- struct{}{}
	status, err = p.Serve(httptest.NewRecorder(), req, "danbooru", "1", "original", false)
	if status != 429 || err == nil {
		t.Fatal("concurrency not limited")
	}
	<-p.slots
}
func TestRejectHTML(t *testing.T) {
	c := &http.Client{Transport: fakeTransport(func(r *http.Request) (*http.Response, error) {
		body := `{"id":1,"file_url":"https://cdn.donmai.us/a.jpg"}`
		typ := "application/json"
		if !strings.HasSuffix(r.URL.Path, ".json") {
			body = "<script>alert(1)</script>"
			typ = "text/html"
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {typ}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	p := New(c, resolver.New(c, time.Minute, 2, "", ""), 1)
	w := httptest.NewRecorder()
	status, e := p.Serve(w, httptest.NewRequest("GET", "/", nil), "danbooru", "1", "original", false)
	if status != 502 || e == nil || w.Body.Len() != 0 {
		t.Fatal("proxied active content")
	}
}
