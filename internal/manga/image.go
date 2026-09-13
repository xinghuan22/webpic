package manga

import (
	"errors"
	"image"
	"image/draw"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"image-gateway/internal/safehttp"

	_ "golang.org/x/image/webp"
)

type Media struct {
	store     *Store
	client    *http.Client
	sem       chan struct{}
	maxBytes  int64
	maxPixels int64
	tempDir   string
}

func NewMedia(store *Store, client *http.Client, concurrency int, maxBytes, maxPixels int64, tempDir string) *Media {
	return &Media{store: store, client: client, sem: make(chan struct{}, concurrency), maxBytes: maxBytes, maxPixels: maxPixels, tempDir: tempDir}
}

func (m *Media) Serve(w http.ResponseWriter, r *http.Request, provider, albumID, chapterID string, index int) (int, error) {
	manifest, err := m.store.Load(provider, albumID)
	if err != nil {
		return http.StatusNotFound, err
	}
	page, err := FindPage(manifest, chapterID, index)
	if err != nil {
		return http.StatusNotFound, err
	}
	u, err := url.Parse(page.SourceURL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.Hostname() == "" {
		return http.StatusBadRequest, errors.New("invalid source URL")
	}
	ctx := safehttp.WithHosts(r.Context(), []string{u.Hostname()})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return http.StatusBadGateway, err
	}
	req.Header.Set("Referer", "https://18comic.vip/")
	resp, err := m.client.Do(req)
	if err != nil {
		return http.StatusBadGateway, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return http.StatusBadGateway, errors.New("manga image upstream failed")
	}
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0]))
	if !strings.HasPrefix(contentType, "image/") || contentType == "image/svg+xml" {
		return http.StatusBadGateway, errors.New("upstream did not return an image")
	}
	if page.Decode.Segments == 0 {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "private, max-age=3600")
		if n := resp.Header.Get("Content-Length"); n != "" {
			w.Header().Set("Content-Length", n)
		}
		w.WriteHeader(http.StatusOK)
		_, err = io.CopyBuffer(w, resp.Body, make([]byte, 32*1024))
		return http.StatusOK, err
	}
	select {
	case m.sem <- struct{}{}:
		defer func() { <-m.sem }()
	default:
		return http.StatusTooManyRequests, errors.New("manga decoder busy")
	}
	return m.decode(w, resp.Body, page.Decode.Segments)
}

func (m *Media) decode(w http.ResponseWriter, src io.Reader, segments int) (int, error) {
	f, err := os.CreateTemp(m.tempDir, "manga-*")
	if err != nil {
		return http.StatusInternalServerError, err
	}
	name := f.Name()
	defer os.Remove(name)
	defer f.Close()
	n, err := io.Copy(f, io.LimitReader(src, m.maxBytes+1))
	if err != nil || n > m.maxBytes {
		return http.StatusBadGateway, errors.New("manga image is too large")
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return http.StatusInternalServerError, err
	}
	cfg, _, err := image.DecodeConfig(f)
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width) > m.maxPixels/int64(cfg.Height) {
		return http.StatusBadGateway, errors.New("invalid manga image dimensions")
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return http.StatusInternalServerError, err
	}
	scrambled, _, err := image.Decode(f)
	if err != nil {
		return http.StatusBadGateway, err
	}
	decoded := Unscramble(scrambled, segments)
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	if err := jpeg.Encode(w, decoded, &jpeg.Options{Quality: 92}); err != nil {
		return http.StatusOK, err
	}
	return http.StatusOK, nil
}

func Unscramble(src image.Image, segments int) image.Image {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	if segments <= 0 {
		draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
		return dst
	}
	move, over := b.Dy()/segments, b.Dy()%segments
	for i := 0; i < segments; i++ {
		h := move
		ySrc := b.Dy() - move*(i+1) - over
		yDst := move * i
		if i == 0 {
			h += over
		} else {
			yDst += over
		}
		draw.Draw(dst, image.Rect(0, yDst, b.Dx(), yDst+h), src, image.Pt(b.Min.X, b.Min.Y+ySrc), draw.Src)
	}
	return dst
}

func ParsePageIndex(value string) (int, error) { return strconv.Atoi(value) }
