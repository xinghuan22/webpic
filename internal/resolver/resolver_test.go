package resolver

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	tests := []struct{ u, site, id, md5 string }{
		{"https://danbooru.donmai.us/posts/11640681", "danbooru", "11640681", ""},
		{"https://gelbooru.com/index.php?page=post&s=view&id=14341716", "gelbooru", "14341716", ""},
		{"https://www.gelbooru.com/index.php?page=post&s=list&md5=ff3f5c2fbaf68f9b489d234c1fe922fb", "gelbooru", "", "ff3f5c2fbaf68f9b489d234c1fe922fb"},
		{"https://yande.re/post/show/123456", "yandere", "123456", ""},
		{"https://konachan.com/post/show/123456/title", "konachan", "123456", ""},
		{"https://www.konachan.com/post/show/123456", "konachan", "123456", ""},
	}
	for _, tt := range tests {
		t.Run(tt.u, func(t *testing.T) {
			r, e := Parse(tt.u)
			if e != nil || r.Site != tt.site || r.ID != tt.id || r.MD5 != tt.md5 {
				t.Fatalf("%+v %v", r, e)
			}
		})
	}
	for _, u := range []string{"http://localhost/posts/1", "http://127.0.0.1/posts/1", "http://[::1]/posts/1", "http://169.254.169.254/posts/1", "https://danbooru.donmai.us.evil.com/posts/1", "https://danbooru.donmai.us@evil.com/posts/1", "https://evil.com@danbooru.donmai.us/posts/1", "https://danbooru.donmai.us:443/posts/1", "file:///posts/1", "https://danbooru.donmai.us/posts/0", "https://danbooru.donmai.us/posts/../1", "https://gelbooru.com/index.php?page=post&s=list&md5=bad"} {
		if _, e := Parse(u); e == nil {
			t.Errorf("accepted %s", u)
		}
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestAdapters(t *testing.T) {
	for _, site := range []string{"danbooru", "gelbooru", "yandere", "konachan"} {
		t.Run(site, func(t *testing.T) {
			calls := 0
			c := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Header.Get("Referer") == "" {
					t.Error("missing referer")
				}
				body := `{"id":42,"file_url":"/original.jpg","sample_url":"/sample.jpg","preview_url":"/preview.jpg","large_file_url":"/large.jpg","preview_file_url":"/small.jpg","width":"800","height":600,"image_width":800,"image_height":600,"score":"10","file_size":12345,"created_at":1700000000,"tags":"one two","tag_string_artist":"artist"}`
				if site == "gelbooru" {
					if r.URL.Query().Get("id") != "42" || r.URL.Query().Get("api_key") != "secret" {
						t.Error("bad DAPI query")
					}
					body = `{"@attributes":{"count":1},"post":[` + body + `]}`
				} else if site != "danbooru" {
					if r.URL.Query().Get("tags") != "id:42" {
						t.Error("bad moebooru query")
					}
					body = "[" + body + "]"
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}
			reg := New(c, time.Minute, 10, "user", "secret")
			for i := 0; i < 2; i++ {
				p, e := reg.Resolve(context.Background(), Reference{Site: site, ID: "42"})
				if e != nil || p.ID != "42" || p.Width != 800 || p.Score != 10 || p.OriginalURL == "" {
					t.Fatalf("%+v %v", p, e)
				}
			}
			if calls != 1 {
				t.Errorf("cache calls=%d", calls)
			}
		})
	}
}
func TestMD5AndUntrustedCDN(t *testing.T) {
	hash := "ff3f5c2fbaf68f9b489d234c1fe922fb"
	for _, bad := range []bool{false, true} {
		c := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			if r.URL.Query().Get("tags") != "md5:"+hash {
				t.Error("missing md5 query")
			}
			file := "https://img3.gelbooru.com/image.jpg"
			if bad {
				file = "http://169.254.169.254/latest/meta-data"
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"post":[{"id":"42","md5":"` + hash + `","file_url":"` + file + `"}]}`))}, nil
		})}
		reg := New(c, time.Minute, 10, "", "")
		p, e := reg.Resolve(context.Background(), Reference{Site: "gelbooru", MD5: hash})
		if bad && e == nil {
			t.Fatal("accepted metadata IP")
		}
		if !bad && (e != nil || p.ID != "42") {
			t.Fatalf("%+v %v", p, e)
		}
	}
}
func TestNoOriginalAsSample(t *testing.T) {
	c := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"id":42,"file_url":"/a.jpg","large_file_url":"/a.jpg","preview_file_url":"/a.jpg"}`))}, nil
	})}
	p, e := New(c, time.Minute, 10, "", "").Resolve(context.Background(), Reference{Site: "danbooru", ID: "42"})
	if e != nil || p.SampleURL != "" || p.PreviewURL != "" {
		t.Fatalf("%+v %v", p, e)
	}
}
