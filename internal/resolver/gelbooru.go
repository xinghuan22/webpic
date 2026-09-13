package resolver

import "net/http"

func newGelbooru(c *http.Client, user, key string) Resolver {
	return &booru{client: c, site: "gelbooru", base: "https://gelbooru.com", user: user, key: key, hosts: []string{"gelbooru.com", "img1.gelbooru.com", "img2.gelbooru.com", "img3.gelbooru.com", "img4.gelbooru.com", "img5.gelbooru.com", "img6.gelbooru.com", "img7.gelbooru.com", "img8.gelbooru.com", "img9.gelbooru.com", "img10.gelbooru.com", "img11.gelbooru.com", "img12.gelbooru.com", "img13.gelbooru.com", "img14.gelbooru.com"}}
}
