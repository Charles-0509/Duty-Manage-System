package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	bindAddr := getEnv("HOT_PROXY_BIND_ADDR", ":8080")
	statePath := getEnv("HOT_PROXY_STATE_PATH", "./data/hot-active-backend.txt")

	transport := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 32,
		IdleConnTimeout:     90 * time.Second,
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target, err := loadTarget(statePath)
		if err != nil {
			http.Error(w, fmt.Sprintf("hot proxy target unavailable: %v", err), http.StatusServiceUnavailable)
			return
		}

		proxy := httputil.NewSingleHostReverseProxy(target)
		proxy.Transport = transport
		proxy.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, proxyErr error) {
			http.Error(rw, fmt.Sprintf("proxy upstream error: %v", proxyErr), http.StatusBadGateway)
		}
		proxy.ServeHTTP(w, r)
	})

	server := &http.Server{
		Addr:              bindAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("hot proxy listening on %s", bindAddr)
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("hot proxy failed: %v", err)
		}
	case <-ctx.Done():
		log.Printf("hot proxy shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("hot proxy shutdown failed: %v", err)
	}
}

func loadTarget(statePath string) (*url.URL, error) {
	content, err := os.ReadFile(statePath)
	if err != nil {
		return nil, err
	}

	raw := strings.TrimSpace(string(content))
	if raw == "" {
		return nil, fmt.Errorf("empty target file")
	}

	target, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if target.Scheme == "" || target.Host == "" {
		return nil, fmt.Errorf("invalid target %q", raw)
	}
	return target, nil
}

func getEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
