package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestProxyHomepageForwardsAllowlistedAsset(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/homepage/assets/site.css" {
			t.Fatalf("upstream request = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=300")
		_, _ = w.Write([]byte("body{color:white}"))
	}))
	defer upstream.Close()
	client := &RiskControlClient{baseURL: upstream.URL, http: upstream.Client()}

	asset, err := client.ProxyHomepage(context.Background(), http.MethodGet, "/assets/site.css")
	if err != nil {
		t.Fatalf("ProxyHomepage() error = %v", err)
	}
	if asset.Status != http.StatusOK || asset.ContentType != "text/css; charset=utf-8" || asset.CacheControl != "public, max-age=300" {
		t.Fatalf("ProxyHomepage() asset = %+v", asset)
	}
	if string(asset.Body) != "body{color:white}" {
		t.Fatalf("ProxyHomepage() body = %q", asset.Body)
	}
}

func TestProxyHomepageAllowsHead(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead || r.URL.Path != "/homepage/" {
			t.Fatalf("upstream request = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	}))
	defer upstream.Close()
	client := &RiskControlClient{baseURL: upstream.URL, http: upstream.Client()}

	asset, err := client.ProxyHomepage(context.Background(), http.MethodHead, "/")
	if err != nil || asset.Status != http.StatusOK || len(asset.Body) != 0 {
		t.Fatalf("ProxyHomepage(HEAD) = %+v, %v", asset, err)
	}
}

func TestProxyHomepageRejectsUnsafeRequestsBeforeCallingUpstream(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls.Add(1)
	}))
	defer upstream.Close()
	client := &RiskControlClient{baseURL: upstream.URL, http: upstream.Client()}

	for _, request := range []struct{ method, path string }{
		{http.MethodPost, "/"},
		{http.MethodGet, "/../api/v1/admin/users"},
		{http.MethodGet, `\..\secret`},
	} {
		if _, err := client.ProxyHomepage(context.Background(), request.method, request.path); !errors.Is(err, ErrInvalidHomepageRequest) {
			t.Fatalf("ProxyHomepage(%s, %q) error = %v", request.method, request.path, err)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("unsafe requests reached upstream %d times", calls.Load())
	}
}

func TestProxyHomepageRejectsOversizedResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", maxHomepageProxyBody+1)))
	}))
	defer upstream.Close()
	client := &RiskControlClient{baseURL: upstream.URL, http: upstream.Client()}

	if _, err := client.ProxyHomepage(context.Background(), http.MethodGet, "/"); !errors.Is(err, ErrHomepageResponseTooLarge) {
		t.Fatalf("ProxyHomepage() error = %v", err)
	}
}
