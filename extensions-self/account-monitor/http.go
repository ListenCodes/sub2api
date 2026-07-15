package accountmonitor

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	ResourceOverview    = "overview"
	ResourceAccounts    = "accounts"
	ResourceAccount     = "account"
	ResourceModels      = "models"
	ResourceUsers       = "users"
	ResourceErrors      = "errors"
	ResourceTrends      = "trends"
	ResourceAttempts    = "attempts"
	ResourceDataQuality = "data-quality"
	ResourceThresholds  = "thresholds"
	ResourceRebuildJobs = "rebuild-jobs"
	ResourceRebuildJob  = "rebuild-job"
)

type AdminRequest struct {
	Resource  string
	Method    string
	ActorID   int64
	AccountID int64
	JobID     int64
	From      time.Time
	To        time.Time
	Page      int
	PageSize  int
	SortBy    string
	SortOrder string
	Query     map[string]string
	Body      json.RawMessage
}

type AdminBackend interface {
	ExecuteAdmin(ctx context.Context, request AdminRequest) (any, error)
}

type Handler struct {
	backend AdminBackend
	webDir  string
	now     func() time.Time
}

func NewHandler(backend AdminBackend, webDir string) *Handler {
	return &Handler{backend: backend, webDir: strings.TrimSpace(webDir), now: func() time.Time { return time.Now().UTC() }}
}

func (h *Handler) ServeAdmin(w http.ResponseWriter, r *http.Request, relativePath string, actorID int64) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if actorID <= 0 {
		writeMonitorError(w, http.StatusForbidden, "administrator identity is required")
		return
	}
	request, status, err := h.parseAdminRequest(r, relativePath, actorID)
	if err != nil {
		writeMonitorError(w, status, err.Error())
		return
	}
	if h.backend == nil {
		writeMonitorError(w, http.StatusServiceUnavailable, "account monitor is unavailable")
		return
	}
	result, err := h.backend.ExecuteAdmin(r.Context(), request)
	if err != nil {
		if errors.Is(err, ErrRebuildOverlap) {
			writeMonitorError(w, http.StatusConflict, err.Error())
			return
		}
		writeMonitorError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeMonitorJSON(w, http.StatusOK, result)
}

func (h *Handler) ServeWeb(w http.ResponseWriter, r *http.Request, relativePath string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeMonitorError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.webDir == "" {
		writeMonitorError(w, http.StatusServiceUnavailable, "account monitor web assets are unavailable")
		return
	}
	clean := filepath.Clean("/" + strings.TrimSpace(relativePath))
	if strings.Contains(relativePath, "\\") || strings.Contains(clean, "..") {
		writeMonitorError(w, http.StatusBadRequest, "invalid asset path")
		return
	}
	if clean == "/" || clean == "/." {
		clean = "/index.html"
	}
	fullPath := filepath.Join(h.webDir, filepath.FromSlash(strings.TrimPrefix(clean, "/")))
	if _, err := os.Stat(fullPath); err != nil {
		writeMonitorError(w, http.StatusNotFound, "asset not found")
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, fullPath)
}

func (h *Handler) parseAdminRequest(r *http.Request, relativePath string, actorID int64) (AdminRequest, int, error) {
	path := strings.Trim(strings.TrimSpace(relativePath), "/")
	parts := strings.Split(path, "/")
	request := AdminRequest{Method: r.Method, ActorID: actorID, Page: 1, PageSize: 20, Query: map[string]string{}}
	for key, values := range r.URL.Query() {
		if len(values) > 0 {
			request.Query[key] = values[0]
		}
	}
	var ok bool
	switch {
	case r.Method == http.MethodGet && path == "overview":
		request.Resource, ok = ResourceOverview, true
	case r.Method == http.MethodGet && path == "accounts":
		request.Resource, ok = ResourceAccounts, true
	case r.Method == http.MethodGet && len(parts) == 2 && parts[0] == "accounts":
		request.Resource, ok = ResourceAccount, true
		request.AccountID, _ = strconv.ParseInt(parts[1], 10, 64)
	case r.Method == http.MethodGet && len(parts) == 3 && parts[0] == "accounts" && parts[2] == "models":
		request.Resource, ok = ResourceModels, true
		request.AccountID, _ = strconv.ParseInt(parts[1], 10, 64)
	case r.Method == http.MethodGet && len(parts) == 3 && parts[0] == "accounts" && parts[2] == "users":
		request.Resource, ok = ResourceUsers, true
		request.AccountID, _ = strconv.ParseInt(parts[1], 10, 64)
	case r.Method == http.MethodGet && len(parts) == 3 && parts[0] == "accounts" && parts[2] == "errors":
		request.Resource, ok = ResourceErrors, true
		request.AccountID, _ = strconv.ParseInt(parts[1], 10, 64)
	case r.Method == http.MethodGet && len(parts) == 3 && parts[0] == "accounts" && parts[2] == "trends":
		request.Resource, ok = ResourceTrends, true
		request.AccountID, _ = strconv.ParseInt(parts[1], 10, 64)
	case r.Method == http.MethodGet && path == "attempts":
		request.Resource, ok = ResourceAttempts, true
	case r.Method == http.MethodGet && path == "data-quality":
		request.Resource, ok = ResourceDataQuality, true
	case (r.Method == http.MethodGet || r.Method == http.MethodPut) && path == "thresholds":
		request.Resource, ok = ResourceThresholds, true
	case r.Method == http.MethodPost && path == "rebuild-jobs":
		request.Resource, ok = ResourceRebuildJobs, true
	case r.Method == http.MethodGet && len(parts) == 2 && parts[0] == "rebuild-jobs":
		request.Resource, ok = ResourceRebuildJob, true
		request.JobID, _ = strconv.ParseInt(parts[1], 10, 64)
	}
	if !ok || (strings.HasPrefix(request.Resource, "account") && request.Resource != ResourceAccounts && request.AccountID <= 0) || (request.Resource == ResourceRebuildJob && request.JobID <= 0) {
		return AdminRequest{}, http.StatusNotFound, errors.New("account monitor endpoint not found")
	}
	if page, err := parsePositiveInt(r.URL.Query().Get("page"), 1); err != nil {
		return AdminRequest{}, http.StatusBadRequest, err
	} else {
		request.Page = page
	}
	if pageSize, err := parsePositiveInt(r.URL.Query().Get("page_size"), 20); err != nil || (pageSize != 20 && pageSize != 50 && pageSize != 100) {
		return AdminRequest{}, http.StatusBadRequest, errors.New("page_size must be one of 20, 50, or 100")
	} else {
		request.PageSize = pageSize
	}
	request.SortBy = strings.TrimSpace(r.URL.Query().Get("sort_by"))
	request.SortOrder = strings.ToLower(strings.TrimSpace(r.URL.Query().Get("sort_order")))
	if request.SortOrder != "asc" {
		request.SortOrder = "desc"
	}
	if request.Resource != ResourceThresholds && request.Resource != ResourceRebuildJobs && request.Resource != ResourceRebuildJob {
		from, to, err := h.parseRange(r)
		if err != nil {
			return AdminRequest{}, http.StatusBadRequest, err
		}
		request.From, request.To = from, to
	}
	if r.Method == http.MethodPut || r.Method == http.MethodPost {
		body, err := io.ReadAll(io.LimitReader(r.Body, 256*1024+1))
		if err != nil || len(body) > 256*1024 || !json.Valid(body) {
			return AdminRequest{}, http.StatusBadRequest, errors.New("invalid JSON request body")
		}
		request.Body = body
		if request.Resource == ResourceRebuildJobs {
			var input struct {
				From time.Time `json:"from"`
				To   time.Time `json:"to"`
			}
			if err := json.Unmarshal(body, &input); err != nil || ValidateRebuildRange(input.From, input.To) != nil {
				return AdminRequest{}, http.StatusBadRequest, errors.New("rebuild range must be valid and no longer than 31 days")
			}
			request.From, request.To = input.From, input.To
		}
	}
	return request, http.StatusOK, nil
}

func (h *Handler) parseRange(r *http.Request) (time.Time, time.Time, error) {
	to := h.now()
	from := to.Add(-24 * time.Hour)
	var err error
	if raw := strings.TrimSpace(r.URL.Query().Get("from")); raw != "" {
		from, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			return time.Time{}, time.Time{}, errors.New("invalid from time")
		}
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("to")); raw != "" {
		to, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			return time.Time{}, time.Time{}, errors.New("invalid to time")
		}
	}
	if !from.Before(to) || to.Sub(from) > 90*24*time.Hour {
		return time.Time{}, time.Time{}, errors.New("time range must be positive and no longer than 90 days")
	}
	return from.UTC(), to.UTC(), nil
}

func parsePositiveInt(raw string, fallback int) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, errors.New("pagination values must be positive integers")
	}
	return value, nil
}

func writeMonitorJSON(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeMonitorError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	writeMonitorJSON(w, status, map[string]string{"error": message})
}
