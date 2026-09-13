package manga

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"io"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func signed(secret, timestamp, nonce string, body []byte) string {
	hash := sha256.Sum256(body)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = io.WriteString(mac, timestamp+"\n"+nonce+"\n"+hex.EncodeToString(hash[:]))
	return hex.EncodeToString(mac.Sum(nil))
}

func TestStoreAuthenticationAndPublicManifest(t *testing.T) {
	secret := "0123456789abcdef0123456789abcdef"
	store, err := NewStore(filepath.Join(t.TempDir(), "manifests"), secret, []string{"jmapiproxy1.cc"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	timestamp := strconv.FormatInt(now.Unix(), 10)
	body := []byte(`{"provider":"jm"}`)
	sig := signed(secret, timestamp, "nonce", body)
	if err := store.Authenticate(timestamp, "nonce", sig, body, now); err != nil {
		t.Fatal(err)
	}
	if err := store.Authenticate(timestamp, "nonce", sig, body, now); err == nil {
		t.Fatal("replay accepted")
	}
	m := Manifest{
		Provider: "jm", AlbumID: "123", Title: "Book",
		Chapters: []Chapter{{
			ID: "456", Title: "One", Order: 1,
			Pages: []Page{{Index: 1, SourceURL: "https://cdn-msp.jmapiproxy1.cc/a.jpg", FileName: "a.jpg", Decode: Decode{Scheme: "jm_vertical_v1", Segments: 10}}},
		}},
	}
	if err := store.Save(m); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load("jm", "123")
	if err != nil {
		t.Fatal(err)
	}
	pub := Public(loaded)
	if pub.Chapters[0].Pages[0].URL != "/manga/media/jm/123/456/1" {
		t.Fatal(pub)
	}
}

func TestUnscrambleMatchesJMStripOrder(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 1, 5))
	colors := []color.RGBA{{4, 0, 0, 255}, {5, 0, 0, 255}, {1, 0, 0, 255}, {2, 0, 0, 255}, {3, 0, 0, 255}}
	for y, c := range colors {
		src.SetRGBA(0, y, c)
	}
	out := Unscramble(src, 2)
	want := []uint8{1, 2, 3, 4, 5}
	for y, v := range want {
		got, _, _, _ := out.At(0, y).RGBA()
		if uint8(got>>8) != v {
			t.Fatalf("row %d: got %d want %d", y, got>>8, v)
		}
	}
}
