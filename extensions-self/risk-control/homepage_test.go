package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	accountmonitor "github.com/ListenCodes/sub2api-account-monitor"
)

type staticPublicGroupReader struct {
	items []accountmonitor.PublicGroup
	err   error
}

func (r staticPublicGroupReader) ReadPublicGroups(context.Context) ([]accountmonitor.PublicGroup, error) {
	return r.items, r.err
}

func newHomepageTestServer(t *testing.T, html string) *HTTPServer {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(html), 0o600); err != nil {
		t.Fatalf("write homepage fixture: %v", err)
	}
	return NewHTTPServer(Config{
		DatabaseURL:    "postgres://risk",
		InternalSecret: "01234567890123456789012345678901",
		HomepageDir:    dir,
	}, NewMemoryRepository(nil))
}

func TestHomepageIsPubliclyServed(t *testing.T) {
	server := newHomepageTestServer(t, "<html><body>homepage-marker</body></html>")
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, httptest.NewRequest(method, "/homepage/", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s /homepage/ status = %d, want 200", method, recorder.Code)
		}
		if method == http.MethodGet && !strings.Contains(recorder.Body.String(), "homepage-marker") {
			t.Fatalf("GET /homepage/ body = %q, want homepage marker", recorder.Body.String())
		}
	}
}

func TestHomepageRejectsWriteMethods(t *testing.T) {
	server := newHomepageTestServer(t, "<html></html>")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/homepage/", nil))
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /homepage/ status = %d, want 405", recorder.Code)
	}
}

func TestHomepagePublicGroups(t *testing.T) {
	server := newHomepageTestServer(t, "<html></html>")
	server.publicGroups = staticPublicGroupReader{items: []accountmonitor.PublicGroup{{
		Name: "GPT Pro", Platform: "openai", RateMultiplier: 0.3,
	}}}
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/homepage/api/public-groups", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET public groups status = %d, want 200", recorder.Code)
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", recorder.Header().Get("Cache-Control"))
	}
	want := `{"groups":[{"name":"GPT Pro","platform":"openai","rate_multiplier":0.3,"peak_rate_enabled":false,"peak_start":"","peak_end":"","peak_rate_multiplier":0}]}`
	if strings.TrimSpace(recorder.Body.String()) != want {
		t.Fatalf("GET public groups body = %q, want %q", recorder.Body.String(), want)
	}
}

func TestHomepagePublicGroupsHEADHasNoBody(t *testing.T) {
	server := newHomepageTestServer(t, "<html></html>")
	server.publicGroups = staticPublicGroupReader{items: []accountmonitor.PublicGroup{{Name: "GPT Pro"}}}
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodHead, "/homepage/api/public-groups", nil))
	if recorder.Code != http.StatusOK || recorder.Body.Len() != 0 {
		t.Fatalf("HEAD public groups status=%d body=%q", recorder.Code, recorder.Body.String())
	}
}

func TestHomepagePublicGroupsRejectsWriteMethods(t *testing.T) {
	server := newHomepageTestServer(t, "<html></html>")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/homepage/api/public-groups", nil))
	if recorder.Code != http.StatusMethodNotAllowed || recorder.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("POST public groups status=%d allow=%q", recorder.Code, recorder.Header().Get("Allow"))
	}
}

func TestHomepagePublicGroupsUnavailableDoesNotLeakSourceError(t *testing.T) {
	for name, reader := range map[string]publicGroupReader{
		"missing": nil,
		"failed":  staticPublicGroupReader{err: errors.New("postgres secret detail")},
	} {
		t.Run(name, func(t *testing.T) {
			server := newHomepageTestServer(t, "<html></html>")
			server.publicGroups = reader
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/homepage/api/public-groups", nil))
			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503", recorder.Code)
			}
			if strings.Contains(recorder.Body.String(), "secret detail") || !strings.Contains(recorder.Body.String(), "public groups unavailable") {
				t.Fatalf("body = %q", recorder.Body.String())
			}
		})
	}
}

func TestHealthIdentifiesExtensionsSelf(t *testing.T) {
	server := newHomepageTestServer(t, "<html></html>")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /healthz status = %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"service":"extensions-self"`) {
		t.Fatalf("GET /healthz body = %q, want extensions-self identity", recorder.Body.String())
	}
}

func TestHealthFailsWhenHomepageIsMissing(t *testing.T) {
	server := NewHTTPServer(Config{
		DatabaseURL:    "postgres://risk",
		InternalSecret: "01234567890123456789012345678901",
		HomepageDir:    t.TempDir(),
	}, NewMemoryRepository(nil))
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET /healthz status = %d, want 503", recorder.Code)
	}
}
