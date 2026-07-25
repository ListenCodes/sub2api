package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	accountmonitor "github.com/ListenCodes/sub2api-account-monitor"
)

func TestNewAccountMonitorRuntimeDisabledDoesNotRequireDatabase(t *testing.T) {
	runtime, err := newAccountMonitorRuntime(context.Background(), Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if runtime != nil {
		t.Fatalf("runtime = %+v, want nil", runtime)
	}
}

func TestNewAccountMonitorRuntimeOpensHomepageSourceWhenMonitorDisabled(t *testing.T) {
	cfg := Config{AccountMonitor: accountmonitor.Config{SourceDatabaseURL: "postgres://source"}}
	runtime, err := newAccountMonitorRuntime(context.Background(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	if runtime == nil || runtime.source == nil {
		t.Fatalf("runtime = %+v, want homepage source", runtime)
	}
	if runtime.handler != nil || runtime.collector != nil {
		t.Fatalf("disabled monitor runtime = %+v, want source only", runtime)
	}
}

type monitorBackendStub struct{ called bool }

func (s *monitorBackendStub) ExecuteAdmin(context.Context, accountmonitor.AdminRequest) (any, error) {
	s.called = true
	return map[string]bool{"ok": true}, nil
}

func TestAccountMonitorAdminRequiresSignatureAndActor(t *testing.T) {
	backend := &monitorBackendStub{}
	monitor := accountmonitor.NewHandler(backend)
	server := NewHTTPServer(Config{InternalSecret: testSecret, Mode: "enforce"}, NewMemoryRepository(defaultRules()), monitor)

	unsigned := serveJSON(server, httptest.NewRequest(http.MethodGet, "/api/v1/admin/account-monitor/overview", nil))
	if unsigned.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned status = %d", unsigned.Code)
	}

	withoutActor := signedRequest(http.MethodGet, "/api/v1/admin/account-monitor/overview", nil, testSecret, "monitor-no-actor", time.Now())
	if response := serveJSON(server, withoutActor); response.Code != http.StatusForbidden {
		t.Fatalf("without actor status = %d", response.Code)
	}

	request := signedRequest(http.MethodGet, "/api/v1/admin/account-monitor/overview", nil, testSecret, "monitor-actor", time.Now())
	request.Header.Set("X-Risk-Actor-ID", "7")
	if response := serveJSON(server, request); response.Code != http.StatusOK {
		t.Fatalf("signed status = %d body=%s", response.Code, response.Body.String())
	}
	if !backend.called {
		t.Fatal("monitor backend was not called")
	}
}

func TestAccountMonitorWebRouteDoesNotExist(t *testing.T) {
	monitor := accountmonitor.NewHandler(&monitorBackendStub{})
	server := NewHTTPServer(Config{InternalSecret: testSecret, Mode: "enforce"}, NewMemoryRepository(defaultRules()), monitor)

	get := serveJSON(server, httptest.NewRequest(http.MethodGet, "/account-monitor/", nil))
	if get.Code != http.StatusNotFound {
		t.Fatalf("GET status=%d body=%q", get.Code, get.Body.String())
	}
	post := serveJSON(server, httptest.NewRequest(http.MethodPost, "/account-monitor/", nil))
	if post.Code != http.StatusNotFound {
		t.Fatalf("POST status=%d", post.Code)
	}
}
