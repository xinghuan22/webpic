package proxy

import (
	"context"
	"errors"
	"fmt"
	"image-gateway/internal/resolver"
	"image-gateway/internal/safehttp"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"
)

type Proxy struct {
	Client   *http.Client
	Registry *resolver.Registry
	slots    chan struct{}
}

func New(c *http.Client, r *resolver.Registry, max int) *Proxy {
	return &Proxy{Client: c, Registry: r, slots: make(chan struct{}, max)}
}

// Serve returns errors only before headers are committed. Copy failures are logged.
func (p *Proxy) Serve(w http.ResponseWriter, r *http.Request, site, id, variant string, download bool) (int, error) {
	if variant != "preview" && variant != "sample" && variant != "original" {
		return 400, resolver.ErrInvalid
	}
	select {
	case p.slots <- struct{}{}:
		defer func() { <-p.slots }()
	default:
		w.Header().Set("Retry-After", "3")
		return 429, resolver.ErrBusy
	}
	start := time.Now()
	status := 502
	var copied int64
	defer func() {
		slog.Info("media proxy", "site", site, "post_id", id, "duration", time.Since(start), "status", status, "bytes", copied)
	}()
	post, err := p.Registry.Resolve(r.Context(), resolver.Reference{Site: site, ID: id})
	if err != nil {
		status = Status(err)
		return status, err
	}
	target := post.OriginalURL
	switch variant {
	case "preview":
		target = post.PreviewURL
	case "sample":
		target = post.SampleURL
		if target == "" {
			target = post.PreviewURL
		}
	}
	if target == "" {
		status = 404
		return status, resolver.ErrNotFound
	}
	a, _ := p.Registry.Adapter(site)
	req, err := http.NewRequestWithContext(safehttp.WithHosts(r.Context(), a.Hosts()), "GET", target, nil)
	if err != nil {
		return 502, errors.New("invalid media URL")
	}
	req.Header.Set("Referer", a.Referer())
	req.Header.Set("User-Agent", safehttp.UserAgent)
	for _, h := range []string{"Range", "If-Range", "If-None-Match", "If-Modified-Since"} {
		if v := r.Header.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}
	resp, err := p.Client.Do(req)
	if err != nil {
		return 502, errors.New("media connection failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 304 {
		for _, h := range []string{"ETag", "Last-Modified"} {
			if v := resp.Header.Get(h); v != "" {
				w.Header().Set(h, v)
			}
		}
		w.Header().Set("Cache-Control", "private, max-age=300")
		status = 304
		w.WriteHeader(status)
		return status, nil
	}
	if resp.StatusCode == 416 {
		w.Header().Set("Content-Range", resp.Header.Get("Content-Range"))
		status = 416
		return status, errors.New("requested range unavailable")
	}
	if resp.StatusCode != 200 && resp.StatusCode != 206 {
		status = 502
		if resp.StatusCode == 404 {
			status = 404
		}
		if resp.StatusCode == 429 {
			status = 429
		}
		return status, errors.New("upstream media unavailable")
	}
	typ, _, e := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if e != nil {
		return 502, errors.New("invalid media content type")
	}
	switch typ {
	case "image/jpeg", "image/png", "image/gif", "image/webp", "image/avif", "image/bmp", "image/tiff", "image/jxl", "video/webm", "video/mp4", "application/octet-stream":
	default:
		return 502, errors.New("unsupported media content type")
	}
	if encoding := resp.Header.Get("Content-Encoding"); encoding != "" && encoding != "identity" {
		return 502, errors.New("unsupported media encoding")
	}
	for _, h := range []string{"Content-Type", "Content-Length", "Last-Modified", "ETag", "Content-Range", "Accept-Ranges"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if download || typ == "application/octet-stream" {
		ext := post.FileExt
		if ext == "" {
			ext = "bin"
		}
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": fmt.Sprintf("%s_%s.%s", site, id, ext)}))
	}
	status = resp.StatusCode
	w.WriteHeader(status)
	copied, err = io.CopyBuffer(w, resp.Body, make([]byte, 32*1024))
	if err != nil {
		slog.Warn("media stream interrupted", "site", site, "post_id", id, "error", "stream copy failed")
	}
	return status, nil
}
func Status(err error) int {
	if errors.Is(err, resolver.ErrInvalid) {
		return 400
	}
	if errors.Is(err, resolver.ErrNotFound) {
		return 404
	}
	if errors.Is(err, resolver.ErrBusy) {
		return 429
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return 504
	}
	var up *resolver.UpstreamError
	if errors.As(err, &up) && up.Status == 429 {
		return 429
	}
	return 502
}

// Avoid leaking query strings containing upstream credentials in logs or errors.
func Message(status int) string {
	switch status {
	case 400:
		return "URL 无效，或暂不支持该站点"
	case 404:
		return "图片已删除、受限或该版本不可用"
	case 429:
		return "请求较多，请稍后重试"
	default:
		return strings.TrimSpace("无法获取图片信息，上游暂时不可用")
	}
}
