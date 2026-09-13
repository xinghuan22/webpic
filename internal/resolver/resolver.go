package resolver

import (
	"context"
	"errors"
	"image-gateway/internal/cache"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var ErrInvalid = errors.New("invalid post URL or identifier")
var ErrNotFound = errors.New("post or image unavailable")
var ErrBusy = errors.New("metadata concurrency limit reached")

type UpstreamError struct{ Status int }

func (e *UpstreamError) Error() string { return "upstream API request failed" }

type ImagePost struct {
	Site        string   `json:"site"`
	ID          string   `json:"id"`
	PostURL     string   `json:"post_url"`
	PreviewURL  string   `json:"preview_url"`
	SampleURL   string   `json:"sample_url"`
	OriginalURL string   `json:"original_url"`
	Width       int      `json:"width"`
	Height      int      `json:"height"`
	FileSize    int64    `json:"file_size"`
	FileExt     string   `json:"file_ext"`
	Rating      string   `json:"rating"`
	Score       int      `json:"score"`
	Source      string   `json:"source"`
	Artists     []string `json:"artists"`
	Characters  []string `json:"characters"`
	Copyrights  []string `json:"copyrights"`
	Tags        []string `json:"tags"`
	CreatedAt   string   `json:"created_at"`
}
type Reference struct{ Site, ID, MD5 string }

var idPattern = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)
var md5Pattern = regexp.MustCompile(`^[a-fA-F0-9]{32}$`)

func ValidID(s string) bool { return idPattern.MatchString(s) }
func Parse(raw string) (Reference, error) {
	u, e := url.Parse(raw)
	if e != nil || len(raw) > 4096 || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Port() != "" {
		return Reference{}, ErrInvalid
	}
	r := Reference{}
	p := strings.Split(strings.Trim(u.Path, "/"), "/")
	host := strings.ToLower(u.Hostname())
	switch host {
	case "danbooru.donmai.us":
		r.Site = "danbooru"
		if len(p) == 2 && p[0] == "posts" {
			r.ID = p[1]
		}
	case "yande.re", "konachan.com", "www.konachan.com":
		r.Site = "konachan"
		if host == "yande.re" {
			r.Site = "yandere"
		}
		if len(p) >= 3 && len(p) <= 4 && p[0] == "post" && p[1] == "show" {
			r.ID = p[2]
		}
	case "gelbooru.com", "www.gelbooru.com":
		r.Site = "gelbooru"
		q := u.Query()
		if (u.Path == "/index.php" || u.Path == "/") && q.Get("page") == "post" {
			if q.Get("s") == "view" {
				r.ID = q.Get("id")
			}
			if q.Get("s") == "list" && md5Pattern.MatchString(q.Get("md5")) {
				r.MD5 = strings.ToLower(q.Get("md5"))
			}
		}
	}
	if r.Site == "" || (!ValidID(r.ID) && r.MD5 == "") {
		return Reference{}, ErrInvalid
	}
	return r, nil
}

type Resolver interface {
	Resolve(context.Context, Reference) (ImagePost, error)
	Hosts() []string
	Referer() string
}
type Registry struct {
	adapters map[string]Resolver
	cache    *cache.Cache[ImagePost]
	slots    chan struct{}
}

func New(client *http.Client, ttl time.Duration, max int, user, key string) *Registry {
	return &Registry{adapters: map[string]Resolver{"danbooru": newDanbooru(client), "gelbooru": newGelbooru(client, user, key), "yandere": newYandere(client), "konachan": newKonachan(client)}, cache: cache.New[ImagePost](ttl, max), slots: make(chan struct{}, 8)}
}
func (r *Registry) Adapter(site string) (Resolver, bool) { a, ok := r.adapters[site]; return a, ok }
func (r *Registry) Resolve(ctx context.Context, ref Reference) (ImagePost, error) {
	a, ok := r.adapters[ref.Site]
	if !ok || (!ValidID(ref.ID) && !(ref.Site == "gelbooru" && md5Pattern.MatchString(ref.MD5))) {
		return ImagePost{}, ErrInvalid
	}
	key := ref.Site + ":" + ref.ID + ":" + ref.MD5
	if p, ok := r.cache.Get(key); ok {
		return p, nil
	}
	select {
	case r.slots <- struct{}{}:
		defer func() { <-r.slots }()
	default:
		return ImagePost{}, ErrBusy
	}
	start := time.Now()
	p, err := a.Resolve(ctx, ref)
	status := 200
	if err != nil {
		status = 502
		if errors.Is(err, ErrNotFound) {
			status = 404
		}
		var up *UpstreamError
		if errors.As(err, &up) {
			status = up.Status
		}
	}
	slog.Info("metadata request", "site", ref.Site, "post_id", ref.ID, "duration", time.Since(start), "status", status, "error", err)
	if err == nil {
		r.cache.Set(key, p)
		r.cache.Set(p.Site+":"+p.ID+":", p)
	}
	return p, err
}
