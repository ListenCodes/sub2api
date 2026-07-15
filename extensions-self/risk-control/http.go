package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	accountmonitor "github.com/ListenCodes/sub2api-account-monitor"
)

type HTTPServer struct {
	service   *RiskService
	repo      RiskRepository
	cfg       Config
	nonces    *nonceStore
	homepage  http.Handler
	homeReady bool
	monitor   *accountmonitor.Handler
}

func NewHTTPServer(cfg Config, repo RiskRepository, monitors ...*accountmonitor.Handler) *HTTPServer {
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = 256 * 1024
	}
	cfg.Mode = normalizeMode(cfg.Mode)
	if strings.TrimSpace(cfg.HomepageDir) == "" {
		cfg.HomepageDir = "/app/homepage"
	}
	info, err := os.Stat(filepath.Join(cfg.HomepageDir, "index.html"))
	homeReady := err == nil && !info.IsDir()
	var monitor *accountmonitor.Handler
	if len(monitors) > 0 {
		monitor = monitors[0]
	}
	return &HTTPServer{
		service:   NewRiskService(cfg, repo),
		repo:      repo,
		cfg:       cfg,
		nonces:    newNonceStore(),
		homepage:  http.StripPrefix("/homepage/", http.FileServer(http.Dir(cfg.HomepageDir))),
		homeReady: homeReady,
		monitor:   monitor,
	}
}

func (s *HTTPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.URL.Path == "/homepage" {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
			return
		}
		http.Redirect(w, r, "/homepage/", http.StatusPermanentRedirect)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/homepage/") {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
			return
		}
		if !s.homeReady {
			writeError(w, http.StatusServiceUnavailable, errors.New("homepage unavailable"))
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=300")
		s.homepage.ServeHTTP(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.URL.Path == "/healthz" {
		if !s.homeReady {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "service": "extensions-self"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "extensions-self"})
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/v1/") {
		writeError(w, http.StatusNotFound, errors.New("not found"))
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, s.cfg.MaxBodyBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if int64(len(body)) > s.cfg.MaxBodyBytes {
		writeError(w, http.StatusRequestEntityTooLarge, errors.New("request body too large"))
		return
	}
	r.Body = io.NopCloser(strings.NewReader(string(body)))
	if err := verifyInternalSignature(r, body, s.cfg.InternalSecret, s.nonces, time.Now().UTC()); err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/v1/admin/") {
		if _, err := actorID(r); err != nil {
			writeError(w, http.StatusForbidden, err)
			return
		}
	}
	s.dispatch(w, r, body)
}

func (s *HTTPServer) dispatch(w http.ResponseWriter, r *http.Request, body []byte) {
	path := strings.TrimSuffix(r.URL.Path, "/")
	switch {
	case strings.HasPrefix(path, "/api/v1/admin/account-monitor"):
		if s.monitor == nil {
			writeError(w, http.StatusServiceUnavailable, errors.New("account monitor unavailable"))
			return
		}
		actor, err := actorID(r)
		if err != nil {
			writeError(w, http.StatusForbidden, err)
			return
		}
		relative := strings.TrimPrefix(path, "/api/v1/admin/account-monitor")
		if relative == "" {
			relative = "/"
		}
		s.monitor.ServeAdmin(w, r, relative, actor)
	case r.Method == http.MethodPost && path == "/api/v1/internal/events/evaluate":
		s.handleEvaluateEvent(w, r, body, true)
	case r.Method == http.MethodPost && path == "/api/v1/internal/events":
		s.handleEvaluateEvent(w, r, body, false)
	case r.Method == http.MethodPost && path == "/api/v1/internal/audit":
		s.handleAuditIngest(w, r, body)
	case r.Method == http.MethodGet && path == "/api/v1/admin/overview":
		s.handleOverview(w, r)
	case r.Method == http.MethodGet && path == "/api/v1/admin/users":
		s.handleUsers(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/api/v1/admin/users/"):
		s.handleUserByPath(w, r, path)
	case r.Method == http.MethodPost && strings.HasPrefix(path, "/api/v1/admin/users/") && strings.HasSuffix(path, "/processed"):
		s.handleMarkUserProcessedByPath(w, r, path)
	case r.Method == http.MethodGet && path == "/api/v1/admin/rules":
		s.handleRules(w, r)
	case r.Method == http.MethodPost && path == "/api/v1/admin/rules":
		s.handleRuleCreate(w, r)
	case r.Method == http.MethodPut && strings.HasPrefix(path, "/api/v1/admin/rules/"):
		s.handleRuleUpdate(w, r, strings.TrimPrefix(path, "/api/v1/admin/rules/"))
	case r.Method == http.MethodPost && path == "/api/v1/admin/rules/test":
		s.handleRuleTest(w, r)
	case r.Method == http.MethodGet && path == "/api/v1/admin/audit":
		s.handleAudit(w, r)
	default:
		writeError(w, http.StatusNotFound, errors.New("not found"))
	}
}

func (s *HTTPServer) handleEvaluateEvent(w http.ResponseWriter, r *http.Request, body []byte, includeDecision bool) {
	var input EventReport
	if err := json.Unmarshal(body, &input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	decision, err := s.service.EvaluateEvent(r.Context(), input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if includeDecision {
		writeJSON(w, http.StatusOK, decision)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true, "event_id": decision.EventID, "decision": decision.Action})
}

func (s *HTTPServer) handleAuditIngest(w http.ResponseWriter, r *http.Request, body []byte) {
	var input AuditReport
	if err := json.Unmarshal(body, &input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(input.Action) == "" || strings.TrimSpace(input.TargetType) == "" || strings.TrimSpace(input.TargetID) == "" {
		writeError(w, http.StatusBadRequest, errors.New("invalid audit record"))
		return
	}
	if err := s.service.RecordAudit(r.Context(), input); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true})
}

func (s *HTTPServer) handleUserByPath(w http.ResponseWriter, r *http.Request, path string) {
	rawID := strings.TrimPrefix(path, "/api/v1/admin/users/")
	userID, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || userID <= 0 {
		writeError(w, http.StatusBadRequest, errors.New("invalid user id"))
		return
	}
	s.handleUser(w, r, userID)
}

func (s *HTTPServer) handleMarkUserProcessedByPath(w http.ResponseWriter, r *http.Request, path string) {
	rawID := strings.TrimSuffix(strings.TrimPrefix(path, "/api/v1/admin/users/"), "/processed")
	userID, err := strconv.ParseInt(strings.Trim(rawID, "/"), 10, 64)
	if err != nil || userID <= 0 {
		writeError(w, http.StatusBadRequest, errors.New("invalid user id"))
		return
	}
	s.handleMarkUserProcessed(w, r, userID)
}
