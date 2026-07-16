package accountmonitor

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	ResourceOverview           = "overview"
	ResourceAccounts           = "accounts"
	ResourceAccount            = "account"
	ResourceModels             = "models"
	ResourceUsers              = "users"
	ResourceErrors             = "errors"
	ResourceTrends             = "trends"
	ResourceAttempts           = "attempts"
	ResourceDataQuality        = "data-quality"
	ResourceThresholds         = "thresholds"
	ResourceRebuildJobs        = "rebuild-jobs"
	ResourceRebuildJob         = "rebuild-job"
	ResourceGroupMonitorGroups = "group-monitor-groups"
	ResourceGroupMonitorGroup  = "group-monitor-group"
)

type AdminRequest struct {
	Resource      string
	Method        string
	ActorID       int64
	AccountID     int64
	GroupID       int64
	JobID         int64
	From          time.Time
	To            time.Time
	Page          int
	PageSize      int
	BucketSeconds int
	SortBy        string
	SortOrder     string
	Query         map[string]string
	Body          json.RawMessage
}

type AdminBackend interface {
	ExecuteAdmin(ctx context.Context, request AdminRequest) (any, error)
}

type Handler struct {
	backend AdminBackend
	now     func() time.Time
}

func NewHandler(backend AdminBackend) *Handler {
	return &Handler{backend: backend, now: func() time.Time { return time.Now().UTC() }}
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
		if errors.Is(err, sql.ErrNoRows) {
			writeMonitorError(w, http.StatusNotFound, "account monitor resource not found")
			return
		}
		if errors.Is(err, ErrRebuildOverlap) {
			writeMonitorError(w, http.StatusConflict, err.Error())
			return
		}
		if errors.Is(err, ErrAccountCandidateLimit) {
			writeMonitorError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		writeMonitorError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeMonitorJSON(w, http.StatusOK, result)
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
	case r.Method == http.MethodGet && path == "group-monitor/groups":
		request.Resource, ok = ResourceGroupMonitorGroups, true
	case r.Method == http.MethodGet && len(parts) == 3 && parts[0] == "group-monitor" && parts[1] == "groups":
		request.Resource, ok = ResourceGroupMonitorGroup, true
		request.GroupID, _ = strconv.ParseInt(parts[2], 10, 64)
	}
	if !ok || (strings.HasPrefix(request.Resource, "account") && request.Resource != ResourceAccounts && request.AccountID <= 0) ||
		(request.Resource == ResourceRebuildJob && request.JobID <= 0) ||
		(request.Resource == ResourceGroupMonitorGroup && request.GroupID <= 0) {
		return AdminRequest{}, http.StatusNotFound, errors.New("account monitor endpoint not found")
	}
	if page, err := parsePositiveInt(r.URL.Query().Get("page"), 1); err != nil {
		return AdminRequest{}, http.StatusBadRequest, err
	} else {
		request.Page = page
	}
	groupMonitor := request.Resource == ResourceGroupMonitorGroups || request.Resource == ResourceGroupMonitorGroup
	defaultPageSize := 20
	if groupMonitor {
		defaultPageSize = 12
	}
	if pageSize, err := parsePositiveInt(r.URL.Query().Get("page_size"), defaultPageSize); err != nil || pageSize < 5 || pageSize > 1000 {
		return AdminRequest{}, http.StatusBadRequest, errors.New("page_size must be an integer from 5 to 1000")
	} else {
		request.PageSize = pageSize
	}
	request.SortBy = strings.TrimSpace(r.URL.Query().Get("sort_by"))
	request.SortOrder = strings.ToLower(strings.TrimSpace(r.URL.Query().Get("sort_order")))
	if request.SortOrder != "asc" {
		request.SortOrder = "desc"
	}
	if request.Resource == ResourceAccounts {
		minScore, hasMin, err := parseRiskScoreFilter(r.URL.Query().Get("min_risk_score"), "min_risk_score")
		if err != nil {
			return AdminRequest{}, http.StatusBadRequest, err
		}
		maxScore, hasMax, err := parseRiskScoreFilter(r.URL.Query().Get("max_risk_score"), "max_risk_score")
		if err != nil {
			return AdminRequest{}, http.StatusBadRequest, err
		}
		if hasMin && hasMax && minScore > maxScore {
			return AdminRequest{}, http.StatusBadRequest, errors.New("min_risk_score must not exceed max_risk_score")
		}
	}
	if request.Resource != ResourceThresholds && request.Resource != ResourceRebuildJobs && request.Resource != ResourceRebuildJob {
		var from, to time.Time
		var err error
		if groupMonitor {
			from, to, request.BucketSeconds, err = h.parseGroupRange(r)
		} else {
			from, to, err = h.parseRange(r)
		}
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

func (h *Handler) parseGroupRange(r *http.Request) (time.Time, time.Time, int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("range"))
	if raw == "" {
		raw = "6h"
	}
	var duration time.Duration
	bucketSeconds := 900
	switch raw {
	case "6h":
		duration, bucketSeconds = 6*time.Hour, 900
	case "24h":
		duration, bucketSeconds = 24*time.Hour, 3600
	case "7d":
		duration, bucketSeconds = 7*24*time.Hour, 25200
	case "30d":
		duration, bucketSeconds = 30*24*time.Hour, 108000
	default:
		return time.Time{}, time.Time{}, 0, errors.New("range must be one of 6h, 24h, 7d, or 30d")
	}
	now := h.now().UTC()
	to := time.Unix(now.Unix()-now.Unix()%int64(bucketSeconds), 0).UTC()
	return to.Add(-duration), to, bucketSeconds, nil
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

func parseRiskScoreFilter(raw, name string) (int, bool, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, false, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 || value > 100 {
		return 0, false, fmt.Errorf("%s must be an integer from 0 to 100", name)
	}
	return value, true, nil
}

func writeMonitorJSON(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeMonitorError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	writeMonitorJSON(w, status, map[string]string{"error": message})
}
