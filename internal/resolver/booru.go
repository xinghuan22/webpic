package resolver

import (
	"context"
	"encoding/json"
	"errors"
	"image-gateway/internal/safehttp"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

type booru struct {
	client                *http.Client
	site, base, user, key string
	hosts                 []string
}

func (b *booru) Hosts() []string { return b.hosts }
func (b *booru) Referer() string { return b.base + "/" }

// Scalar accepts the string-or-number fields emitted by different booru versions.
type scalar string

func (s *scalar) UnmarshalJSON(raw []byte) error {
	if string(raw) == "null" {
		return nil
	}
	if len(raw) > 0 && raw[0] == '"' {
		var v string
		if e := json.Unmarshal(raw, &v); e != nil {
			return e
		}
		*s = scalar(v)
		return nil
	}
	var n json.Number
	if e := json.Unmarshal(raw, &n); e != nil {
		return e
	}
	*s = scalar(n.String())
	return nil
}
func (s scalar) integer() int64 { v, _ := strconv.ParseInt(string(s), 10, 64); return v }

type record struct {
	ID          scalar `json:"id"`
	MD5         string `json:"md5"`
	File        string `json:"file_url"`
	Sample      string `json:"sample_url"`
	Large       string `json:"large_file_url"`
	Preview     string `json:"preview_url"`
	PreviewFile string `json:"preview_file_url"`
	Width       scalar `json:"width"`
	Height      scalar `json:"height"`
	ImageWidth  scalar `json:"image_width"`
	ImageHeight scalar `json:"image_height"`
	Size        scalar `json:"file_size"`
	Ext         string `json:"file_ext"`
	Rating      string `json:"rating"`
	Score       scalar `json:"score"`
	Source      string `json:"source"`
	Tags        string `json:"tags"`
	General     string `json:"tag_string_general"`
	Artist      string `json:"tag_string_artist"`
	Character   string `json:"tag_string_character"`
	Copyright   string `json:"tag_string_copyright"`
	Created     scalar `json:"created_at"`
}

func (b *booru) Resolve(ctx context.Context, ref Reference) (ImagePost, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	endpoint := b.base + "/post.json"
	q := url.Values{"tags": {"id:" + ref.ID}, "limit": {"1"}, "api_version": {"2"}, "include_tags": {"1"}}
	switch b.site {
	case "danbooru":
		endpoint = b.base + "/posts/" + ref.ID + ".json"
		q = url.Values{}
	case "gelbooru":
		endpoint = b.base + "/index.php"
		q = url.Values{"page": {"dapi"}, "s": {"post"}, "q": {"index"}, "json": {"1"}, "limit": {"1"}}
		if ref.MD5 != "" {
			q.Set("tags", "md5:"+ref.MD5)
		} else {
			q.Set("id", ref.ID)
		}
		if b.user != "" {
			q.Set("user_id", b.user)
		}
		if b.key != "" {
			q.Set("api_key", b.key)
		}
	}
	req, err := http.NewRequestWithContext(safehttp.WithHosts(ctx, []string{strings.TrimPrefix(b.base, "https://")}), "GET", endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return ImagePost{}, errors.New("invalid API endpoint")
	}
	req.Header.Set("Referer", b.Referer())
	req.Header.Set("User-Agent", safehttp.UserAgent)
	resp, err := b.client.Do(req)
	if err != nil {
		return ImagePost{}, errors.New("API connection failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return ImagePost{}, ErrNotFound
	}
	if resp.StatusCode != 200 {
		return ImagePost{}, &UpstreamError{resp.StatusCode}
	}
	limited := &io.LimitedReader{R: resp.Body, N: 2*1024*1024 + 1}
	var raw json.RawMessage
	dec := json.NewDecoder(limited)
	if dec.Decode(&raw) != nil || limited.N <= 0 {
		return ImagePost{}, errors.New("invalid or oversized API response")
	}
	var tail any
	if dec.Decode(&tail) != io.EOF {
		return ImagePost{}, errors.New("invalid API response trailer")
	}
	var rows []record
	var tagTypes map[string]string
	if b.site == "danbooru" {
		var row record
		err = json.Unmarshal(raw, &row)
		rows = []record{row}
	} else if len(raw) > 0 && raw[0] == '[' {
		err = json.Unmarshal(raw, &rows)
	} else {
		var wrapped struct {
			Post     []record          `json:"post"`
			Posts    []record          `json:"posts"`
			TagTypes map[string]string `json:"tags"`
		}
		err = json.Unmarshal(raw, &wrapped)
		rows = wrapped.Post
		if len(rows) == 0 {
			rows = wrapped.Posts
		}
		tagTypes = wrapped.TagTypes
	}
	if err != nil {
		return ImagePost{}, errors.New("unsupported API response format")
	}
	if len(rows) == 0 {
		return ImagePost{}, ErrNotFound
	}
	v := rows[0]
	if !ValidID(string(v.ID)) || (ref.ID != "" && ref.ID != string(v.ID)) || (ref.MD5 != "" && !strings.EqualFold(ref.MD5, v.MD5)) {
		return ImagePost{}, errors.New("API post identity mismatch")
	}
	p := ImagePost{Site: b.site, ID: string(v.ID), OriginalURL: v.File, SampleURL: v.Sample, PreviewURL: v.Preview, Width: int(v.Width.integer()), Height: int(v.Height.integer()), FileSize: v.Size.integer(), FileExt: v.Ext, Rating: v.Rating, Score: int(v.Score.integer()), Source: v.Source, Tags: strings.Fields(v.Tags), Artists: strings.Fields(v.Artist), Characters: strings.Fields(v.Character), Copyrights: strings.Fields(v.Copyright), CreatedAt: string(v.Created)}
	p.PostURL = b.base + "/post/show/" + p.ID
	if b.site == "danbooru" {
		p.PostURL = b.base + "/posts/" + p.ID
		p.SampleURL = v.Large
		p.PreviewURL = v.PreviewFile
		p.Width = int(v.ImageWidth.integer())
		p.Height = int(v.ImageHeight.integer())
		p.Tags = strings.Fields(v.General)
	}
	if b.site == "gelbooru" {
		p.PostURL = b.base + "/index.php?page=post&s=view&id=" + p.ID
	}
	for _, field := range []*string{&p.OriginalURL, &p.SampleURL, &p.PreviewURL} {
		if *field == "" {
			continue
		}
		u, e := url.Parse(*field)
		if e != nil {
			return ImagePost{}, errors.New("invalid media URL")
		}
		base, _ := url.Parse(b.base)
		u = base.ResolveReference(u)
		if safehttp.Validate(u, b.hosts) != nil {
			return ImagePost{}, errors.New("unapproved media CDN")
		}
		*field = u.String()
	}
	// A sample identical to the original must never trigger an automatic original download.
	if p.SampleURL == p.OriginalURL {
		p.SampleURL = ""
	}
	if p.PreviewURL == p.OriginalURL {
		p.PreviewURL = ""
	}
	if p.FileExt == "" {
		u, _ := url.Parse(p.OriginalURL)
		p.FileExt = strings.TrimPrefix(path.Ext(u.Path), ".")
	}
	if len(p.FileExt) > 10 || strings.ContainsAny(p.FileExt, "/\\\r\n\"") {
		p.FileExt = "bin"
	}
	if ts, e := strconv.ParseInt(p.CreatedAt, 10, 64); e == nil && ts > 0 {
		p.CreatedAt = time.Unix(ts, 0).UTC().Format(time.RFC3339)
	}
	if (b.site == "yandere" || b.site == "konachan") && len(tagTypes) > 0 {
		p.Artists = nil
		p.Characters = nil
		p.Copyrights = nil
		for _, tag := range p.Tags {
			switch tagTypes[tag] {
			case "artist":
				p.Artists = append(p.Artists, tag)
			case "character":
				p.Characters = append(p.Characters, tag)
			case "copyright":
				p.Copyrights = append(p.Copyrights, tag)
			}
		}
	}
	if b.site == "gelbooru" && len(p.Tags) > 0 {
		artists, characters, copyrights, classifyErr := b.classifyGelbooruTags(ctx, p.Tags)
		p.Artists = artists
		p.Characters = characters
		p.Copyrights = copyrights
		if classifyErr != nil {
			// Tag classification enriches the page but is not required to proxy the image.
			// Keep the post usable when Gelbooru's tag endpoint is temporarily unavailable.
			slog.Warn("gelbooru tag classification failed", "post_id", p.ID, "error", classifyErr)
		}
	}
	return p, nil
}
