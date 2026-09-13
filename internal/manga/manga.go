package manga

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var idPattern = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)

type Decode struct {
	Scheme   string `json:"scheme"`
	Segments int    `json:"segments"`
}

type Page struct {
	Index     int    `json:"index"`
	SourceURL string `json:"source_url"`
	FileName  string `json:"file_name"`
	Decode    Decode `json:"decode"`
}

type Chapter struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Order int    `json:"order"`
	Pages []Page `json:"pages"`
}

type Manifest struct {
	Provider    string    `json:"provider"`
	AlbumID     string    `json:"album_id"`
	Title       string    `json:"title"`
	Authors     []string  `json:"authors"`
	Description string    `json:"description"`
	Chapters    []Chapter `json:"chapters"`
	UpdatedAt   int64     `json:"updated_at"`
}

type PublicPage struct {
	Index int    `json:"index"`
	URL   string `json:"url"`
}

type PublicChapter struct {
	ID    string       `json:"id"`
	Title string       `json:"title"`
	Order int          `json:"order"`
	Pages []PublicPage `json:"pages"`
}

type PublicManifest struct {
	Provider    string          `json:"provider"`
	AlbumID     string          `json:"album_id"`
	Title       string          `json:"title"`
	Authors     []string        `json:"authors"`
	Description string          `json:"description"`
	Chapters    []PublicChapter `json:"chapters"`
	UpdatedAt   int64           `json:"updated_at"`
}

type Store struct {
	dir    string
	secret []byte
	mu     sync.RWMutex
	nonces map[string]time.Time
}

func NewStore(dir, secret string) (*Store, error) {
	if len(secret) < 32 {
		return nil, errors.New("MANGA_PUBLISH_SECRET must contain at least 32 characters")
	}
	if err := os.MkdirAll(filepath.Join(dir, "jm"), 0o700); err != nil {
		return nil, fmt.Errorf("create manga manifest directory: %w", err)
	}
	return &Store{dir: dir, secret: []byte(secret), nonces: make(map[string]time.Time)}, nil
}

func (s *Store) Authenticate(timestamp, nonce, signature string, body []byte, now time.Time) error {
	unix, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || nonce == "" || len(nonce) > 128 || len(signature) != sha256.Size*2 {
		return errors.New("invalid publish authentication")
	}
	requestTime := time.Unix(unix, 0)
	if requestTime.Before(now.Add(-5*time.Minute)) || requestTime.After(now.Add(5*time.Minute)) {
		return errors.New("expired publish request")
	}
	bodyHash := sha256.Sum256(body)
	message := timestamp + "\n" + nonce + "\n" + hex.EncodeToString(bodyHash[:])
	mac := hmac.New(sha256.New, s.secret)
	_, _ = io.WriteString(mac, message)
	want := mac.Sum(nil)
	got, err := hex.DecodeString(signature)
	if err != nil || !hmac.Equal(got, want) {
		return errors.New("invalid publish signature")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for key, expires := range s.nonces {
		if now.After(expires) {
			delete(s.nonces, key)
		}
	}
	if _, exists := s.nonces[nonce]; exists {
		return errors.New("publish request replayed")
	}
	s.nonces[nonce] = now.Add(10 * time.Minute)
	return nil
}

func (s *Store) Validate(m *Manifest) error {
	if m.Provider != "jm" || !idPattern.MatchString(m.AlbumID) || strings.TrimSpace(m.Title) == "" {
		return errors.New("invalid manga manifest")
	}
	if len(m.Title) > 500 || len(m.Description) > 5000 || len(m.Authors) > 100 {
		return errors.New("manga metadata is too large")
	}
	if len(m.Chapters) == 0 || len(m.Chapters) > 500 {
		return errors.New("invalid chapter count")
	}
	total := 0
	for ci := range m.Chapters {
		chapter := &m.Chapters[ci]
		if !idPattern.MatchString(chapter.ID) || len(chapter.Pages) == 0 || len(chapter.Pages) > 2000 {
			return errors.New("invalid chapter")
		}
		if len(chapter.Title) > 500 {
			return errors.New("chapter title is too large")
		}
		total += len(chapter.Pages)
		for pi := range chapter.Pages {
			page := &chapter.Pages[pi]
			if page.Index != pi+1 || page.Decode.Segments < 0 || page.Decode.Segments > 40 {
				return errors.New("invalid manga page")
			}
			if page.Decode.Segments > 0 && page.Decode.Scheme != "jm_vertical_v1" {
				return errors.New("unsupported manga decode scheme")
			}
			u, err := url.Parse(page.SourceURL)
			if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.Hostname() == "" {
				return errors.New("unapproved manga image URL")
			}
		}
	}
	if total > 10000 {
		return errors.New("manga contains too many pages")
	}
	sort.SliceStable(m.Chapters, func(i, j int) bool { return m.Chapters[i].Order < m.Chapters[j].Order })
	m.UpdatedAt = time.Now().Unix()
	return nil
}

func (s *Store) Save(m Manifest) error {
	if err := s.Validate(&m); err != nil {
		return err
	}
	body, err := json.Marshal(m)
	if err != nil {
		return err
	}
	dir := filepath.Join(s.dir, m.Provider)
	tmp, err := os.CreateTemp(dir, ".manifest-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err = tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(body)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmpName, filepath.Join(dir, m.AlbumID+".json"))
}

func (s *Store) Load(provider, albumID string) (Manifest, error) {
	if provider != "jm" || !idPattern.MatchString(albumID) {
		return Manifest{}, errors.New("invalid manga identifier")
	}
	body, err := os.ReadFile(filepath.Join(s.dir, provider, albumID+".json"))
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := json.Unmarshal(body, &m); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

func FindPage(m Manifest, chapterID string, index int) (Page, error) {
	if !idPattern.MatchString(chapterID) || index < 1 {
		return Page{}, errors.New("invalid manga page")
	}
	for _, chapter := range m.Chapters {
		if chapter.ID == chapterID && index <= len(chapter.Pages) {
			return chapter.Pages[index-1], nil
		}
	}
	return Page{}, os.ErrNotExist
}

func Public(m Manifest) PublicManifest {
	out := PublicManifest{Provider: m.Provider, AlbumID: m.AlbumID, Title: m.Title, Authors: m.Authors, Description: m.Description, UpdatedAt: m.UpdatedAt}
	for _, chapter := range m.Chapters {
		pub := PublicChapter{ID: chapter.ID, Title: chapter.Title, Order: chapter.Order}
		for _, page := range chapter.Pages {
			pub.Pages = append(pub.Pages, PublicPage{Index: page.Index, URL: fmt.Sprintf("/manga/media/%s/%s/%s/%d", m.Provider, m.AlbumID, chapter.ID, page.Index)})
		}
		out.Chapters = append(out.Chapters, pub)
	}
	return out
}
