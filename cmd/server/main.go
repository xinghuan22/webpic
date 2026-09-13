package main

import (
	"context"
	"errors"
	"fmt"
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
	router := server.New(reg, proxy.New(client, reg, number("MAX_PROXY_CONCURRENCY", 16, 1, 128)))
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
