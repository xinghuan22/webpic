package main

import (
	"context"
	"errors"
	"fmt"
	"image-gateway/internal/manga"
	"image-gateway/internal/proxy"
	"image-gateway/internal/resolver"
	"image-gateway/internal/safehttp"
	"image-gateway/internal/server"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

func number(name string, def, min, max int) int {
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	n, e := strconv.Atoi(v)
	if e != nil || n < min || n > max {
		fmt.Fprintf(os.Stderr, "invalid %s (expected %d..%d)\n", name, min, max)
		os.Exit(1)
	}
	return n
}
func main() {
	level := slog.LevelInfo
	switch os.Getenv("LOG_LEVEL") {
	case "debug":
		level = slog.LevelDebug
	case "", "info":
	default:
		fmt.Fprintln(os.Stderr, "invalid LOG_LEVEL")
		os.Exit(1)
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})))
	listen := os.Getenv("LISTEN")
	if listen == "" {
		listen = ":8080"
	}
	client := safehttp.New()
	reg := resolver.New(client, time.Duration(number("METADATA_TTL_SECONDS", 1200, 1, 86400))*time.Second, number("METADATA_CACHE_MAX", 1000, 1, 10000), os.Getenv("GELBOORU_USER_ID"), os.Getenv("GELBOORU_API_KEY"))
	var mangaRoutes *manga.Routes
	if secret := os.Getenv("MANGA_PUBLISH_SECRET"); secret != "" {
		dir := os.Getenv("MANGA_MANIFEST_DIR")
		if dir == "" {
			dir = "data/manga/manifests"
		}
		store, err := manga.NewStore(dir, secret)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		base := os.Getenv("PUBLIC_BASE_URL")
		if base == "" {
			base = "http://localhost" + listen
		}
		mangaRoutes = &manga.Routes{Store: store, Media: manga.NewMedia(store, client, number("MAX_MANGA_DECODE_CONCURRENCY", 2, 1, 8), int64(number("MAX_MANGA_IMAGE_MIB", 24, 1, 100))*1024*1024, int64(number("MAX_MANGA_MEGAPIXELS", 40, 1, 100))*1000000, "/tmp"), PublicBase: base}
	} else {
		slog.Warn("manga reader disabled", "reason", "MANGA_PUBLISH_SECRET is empty")
	}
	router := server.New(reg, proxy.New(client, reg, number("MAX_PROXY_CONCURRENCY", 16, 1, 128)), mangaRoutes)
	s := &http.Server{Addr: listen, Handler: router, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 190 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() { slog.Info("server listening", "address", listen); done <- s.ListenAndServe() }()
	select {
	case err := <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := s.Shutdown(shutdown); err != nil {
			_ = s.Close()
		}
	}
}
