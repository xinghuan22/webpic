package resolver

import "net/http"

func newKonachan(c *http.Client) Resolver {
	return &booru{client: c, site: "konachan", base: "https://konachan.com", hosts: []string{"konachan.com", "www.konachan.com"}}
}
