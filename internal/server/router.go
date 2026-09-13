package server

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"github.com/gin-gonic/gin"
	"html/template"
	"image-gateway/internal/proxy"
	"image-gateway/internal/resolver"
	"image-gateway/web"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

func New(reg *resolver.Registry, media *proxy.Proxy) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	_ = r.SetTrustedProxies(nil)
	funcs := template.FuncMap{"size": func(n int64) string {
		if n <= 0 {
			return "未知"
		}
		if n < 1024*1024 {
			return fmt.Sprintf("%.1f KB", float64(n)/1024)
		}
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}, "join": func(v []string) string {
		if len(v) == 0 {
			return "—"
		}
		return strings.Join(v, ", ")
	}, "upper": strings.ToUpper}
	r.SetHTMLTemplate(template.Must(template.New("").Funcs(funcs).ParseFS(web.Files, "templates/*.html")))
	r.Use(func(c *gin.Context) {
		start := time.Now()
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("Content-Security-Policy", "default-src 'none'; img-src 'self'; style-src 'self'; script-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
		defer func() {
			if recover() != nil {
				slog.Error("request panic", "route", c.FullPath())
				if !c.Writer.Written() {
					fail(c, 500)
				}
				c.Abort()
			}
			slog.Info("http request", "route", c.FullPath(), "status", c.Writer.Status(), "duration", time.Since(start))
		}()
		c.Next()
	})
	static, _ := fs.Sub(web.Files, "static")
	r.StaticFS("/static", http.FS(static))
	r.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })
	r.GET("/", func(c *gin.Context) { c.HTML(200, "home.html", nil) })
	resolve := func(c *gin.Context) (resolver.ImagePost, error) {
		var ref resolver.Reference
		var err error
		if c.Param("site") != "" {
			ref = resolver.Reference{Site: c.Param("site"), ID: c.Param("id")}
		} else {
			ref, err = resolver.Parse(c.Query("url"))
		}
		if err != nil {
			return resolver.ImagePost{}, err
		}
		return reg.Resolve(c.Request.Context(), ref)
	}
	r.GET("/api/resolve", func(c *gin.Context) {
		p, e := resolve(c)
		if e != nil {
			fail(c, proxy.Status(e))
			return
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(200, p)
	})
	view := func(c *gin.Context) {
		p, e := resolve(c)
		if e != nil {
			fail(c, proxy.Status(e))
			return
		}
		variant, upgradeVariant := "", ""
		if p.PreviewURL != "" {
			variant = "preview"
			if p.SampleURL != "" && p.SampleURL != p.PreviewURL {
				upgradeVariant = "sample"
			}
		} else if p.SampleURL != "" {
			variant = "sample"
		}
		c.Header("Cache-Control", "no-store")
		c.HTML(200, "post.html", gin.H{"Post": p, "Variant": variant, "UpgradeVariant": upgradeVariant})
	}
	r.GET("/view", view)
	r.GET("/post/:site/:id", view)
	serve := func(download bool) gin.HandlerFunc {
		return func(c *gin.Context) {
			variant := c.Param("variant")
			if download {
				variant = "original"
			}
			status, e := media.Serve(c.Writer, c.Request, c.Param("site"), c.Param("id"), variant, download)
			if e != nil {
				slog.Warn("media request failed", "site", c.Param("site"), "post_id", c.Param("id"), "error", e)
				fail(c, status)
			}
		}
	}
	r.GET("/media/:site/:id/:variant", serve(false))
	r.GET("/download/:site/:id", serve(true))
	r.NoRoute(func(c *gin.Context) { fail(c, 404) })
	return r
}
func fail(c *gin.Context, status int) {
	var b [8]byte
	_, _ = rand.Read(b[:])
	id := hex.EncodeToString(b[:])
	message := proxy.Message(status)
	slog.Warn("request failed", "error_id", id, "status", status, "site", c.Param("site"), "post_id", c.Param("id"))
	c.Header("Cache-Control", "no-store")
	if strings.HasPrefix(c.Request.URL.Path, "/api/") {
		c.JSON(status, gin.H{"error": gin.H{"message": message, "id": id}})
	} else {
		c.HTML(status, "error.html", gin.H{"Message": message, "ErrorID": id})
	}
}
