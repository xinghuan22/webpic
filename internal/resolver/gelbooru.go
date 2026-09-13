package resolver

import (
	"context"
	"encoding/json"
	"errors"
	"image-gateway/internal/safehttp"
	"io"
	"net/http"
	"net/url"
	"strings"
)

func newGelbooru(c *http.Client, user, key string) Resolver {
	return &booru{client: c, site: "gelbooru", base: "https://gelbooru.com", user: user, key: key, hosts: []string{"gelbooru.com", "img1.gelbooru.com", "img2.gelbooru.com", "img3.gelbooru.com", "img4.gelbooru.com", "img5.gelbooru.com", "img6.gelbooru.com", "img7.gelbooru.com", "img8.gelbooru.com", "img9.gelbooru.com", "img10.gelbooru.com", "img11.gelbooru.com", "img12.gelbooru.com", "img13.gelbooru.com", "img14.gelbooru.com"}}
}

type gelbooruTag struct {
	Name string `json:"name"`
	Type scalar `json:"type"`
}

// Gelbooru post responses contain one flat tag string. Resolve tag metadata in
// bounded batches so the unified post can expose artist, character and copyright.
func (b *booru) classifyGelbooruTags(ctx context.Context, tags []string) (artists, characters, copyrights []string, resultErr error) {
	const batchSize = 50
	types := make(map[string]int64, len(tags))
	for start := 0; start < len(tags); start += batchSize {
		end := min(start+batchSize, len(tags))
		q := url.Values{
			"page":  {"dapi"},
			"s":     {"tag"},
			"q":     {"index"},
			"json":  {"1"},
			"limit": {"100"},
			"names": {strings.Join(tags[start:end], " ")},
		}
		if b.user != "" {
			q.Set("user_id", b.user)
		}
		if b.key != "" {
			q.Set("api_key", b.key)
		}
		req, err := http.NewRequestWithContext(safehttp.WithHosts(ctx, []string{"gelbooru.com"}), http.MethodGet, b.base+"/index.php?"+q.Encode(), nil)
		if err != nil {
			return artists, characters, copyrights, errors.New("invalid Gelbooru tag API endpoint")
		}
		req.Header.Set("Referer", b.Referer())
		req.Header.Set("User-Agent", safehttp.UserAgent)
		resp, err := b.client.Do(req)
		if err != nil {
			return artists, characters, copyrights, errors.New("Gelbooru tag API connection failed")
		}
		rows, decodeErr := decodeGelbooruTags(resp)
		resp.Body.Close()
		if decodeErr != nil {
			return artists, characters, copyrights, decodeErr
		}
		for _, row := range rows {
			types[row.Name] = row.Type.integer()
		}
	}

	for _, tag := range tags {
		switch types[tag] {
		case 1:
			artists = append(artists, tag)
		case 3:
			copyrights = append(copyrights, tag)
		case 4:
			characters = append(characters, tag)
		}
	}
	return artists, characters, copyrights, nil
}

func decodeGelbooruTags(resp *http.Response) ([]gelbooruTag, error) {
	if resp.StatusCode != http.StatusOK {
		return nil, &UpstreamError{Status: resp.StatusCode}
	}
	limited := &io.LimitedReader{R: resp.Body, N: 2*1024*1024 + 1}
	var raw json.RawMessage
	if err := json.NewDecoder(limited).Decode(&raw); err != nil || limited.N <= 0 {
		return nil, errors.New("invalid or oversized Gelbooru tag API response")
	}
	if len(raw) > 0 && raw[0] == '[' {
		var rows []gelbooruTag
		if err := json.Unmarshal(raw, &rows); err != nil {
			return nil, errors.New("unsupported Gelbooru tag API response")
		}
		return rows, nil
	}
	var wrapped struct {
		Tags json.RawMessage `json:"tag"`
	}
	if err := json.Unmarshal(raw, &wrapped); err != nil || len(wrapped.Tags) == 0 || string(wrapped.Tags) == "null" {
		return nil, errors.New("unsupported Gelbooru tag API response")
	}
	if wrapped.Tags[0] == '[' {
		var rows []gelbooruTag
		if err := json.Unmarshal(wrapped.Tags, &rows); err != nil {
			return nil, errors.New("unsupported Gelbooru tag API response")
		}
		return rows, nil
	}
	var row gelbooruTag
	if err := json.Unmarshal(wrapped.Tags, &row); err != nil {
		return nil, errors.New("unsupported Gelbooru tag API response")
	}
	return []gelbooruTag{row}, nil
}
