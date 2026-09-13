package resolver

import "net/http"

func newYandere(c *http.Client) Resolver {
	return &booru{client: c, site: "yandere", base: "https://yande.re", hosts: []string{"yande.re", "files.yande.re", "assets.yande.re"}}
}
