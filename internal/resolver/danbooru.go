package resolver

import "net/http"

func newDanbooru(c *http.Client) Resolver {
	return &booru{client: c, site: "danbooru", base: "https://danbooru.donmai.us", hosts: []string{"danbooru.donmai.us", "cdn.donmai.us"}}
}
