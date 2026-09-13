package manga

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type Routes struct {
	Store      *Store
	Media      *Media
	PublicBase string
}

func (m *Routes) Register(r *gin.Engine) {
	r.POST("/api/manga/publish", m.publish)
	r.GET("/api/manga/:provider/:album", m.metadata)
	r.GET("/manga/:provider/:album", m.reader)
	r.GET("/manga/media/:provider/:album/:chapter/:page", m.media)
}

func (m *Routes) publish(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 5<<20)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		mangaError(c, http.StatusRequestEntityTooLarge, "发布内容过大")
		return
	}
	if err = m.Store.Authenticate(c.GetHeader("X-Manga-Timestamp"), c.GetHeader("X-Manga-Nonce"), c.GetHeader("X-Manga-Signature"), body, time.Now()); err != nil {
		mangaError(c, http.StatusUnauthorized, "发布认证失败")
		return
	}
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&manifest); err != nil {
		mangaError(c, http.StatusBadRequest, "漫画清单格式错误")
		return
	}
	if err = m.Store.Save(manifest); err != nil {
		slog.Warn("manga publish rejected", "error", err)
		mangaError(c, http.StatusBadRequest, "漫画清单无效")
		return
	}
	path := "/manga/" + manifest.Provider + "/" + manifest.AlbumID
	c.JSON(http.StatusOK, gin.H{"path": path, "url": strings.TrimRight(m.PublicBase, "/") + path})
}

func (m *Routes) metadata(c *gin.Context) {
	manifest, err := m.Store.Load(c.Param("provider"), c.Param("album"))
	if err != nil {
		mangaError(c, http.StatusNotFound, "漫画不存在或尚未发布")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, Public(manifest))
}

func (m *Routes) reader(c *gin.Context) {
	if _, err := m.Store.Load(c.Param("provider"), c.Param("album")); err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.HTML(http.StatusOK, "manga.html", gin.H{"Provider": c.Param("provider"), "AlbumID": c.Param("album")})
}

func (m *Routes) media(c *gin.Context) {
	index, err := ParsePageIndex(c.Param("page"))
	if err != nil {
		mangaError(c, http.StatusBadRequest, "页码无效")
		return
	}
	status, err := m.Media.Serve(c.Writer, c.Request, c.Param("provider"), c.Param("album"), c.Param("chapter"), index)
	if err != nil && !c.Writer.Written() {
		slog.Warn("manga media failed", "error", err)
		mangaError(c, status, "图片加载失败")
	}
}

func mangaError(c *gin.Context, status int, message string) {
	if c.Request.Context().Err() != nil {
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(status, gin.H{"error": gin.H{"message": message}})
}
