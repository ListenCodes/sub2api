package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
