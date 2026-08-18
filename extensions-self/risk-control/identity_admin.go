package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
)

func identityUserRoute(path string) (int64, string, bool, bool) {
	relative := strings.TrimPrefix(path, "/api/v1/admin/users/")
	parts := strings.Split(strings.Trim(relative, "/"), "/")
	if len(parts) != 2 && len(parts) != 3 {
		return 0, "", false, false
	}
	userID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || userID <= 0 {
		return 0, "", false, false
	}
	if len(parts) == 3 {
		if parts[1] == "ip-identities" && parts[2] == "search" {
			return userID, parts[1], true, true
		}
		return 0, "", false, false
	}
	switch parts[1] {
	case "identity-summary", "ip-identities", "device-identities", "associated-users":
		return userID, parts[1], false, true
	default:
		return 0, "", false, false
	}
}

func (s *HTTPServer) handleIdentityUser(w http.ResponseWriter, r *http.Request, userID int64, section string, exactSearch bool) {
	if s.identity == nil || !s.cfg.Identity.AdminEnabled {
		writeError(w, http.StatusServiceUnavailable, errors.New("identity admin is disabled"))
		return
	}
	limit, offset := identityPagination(r)
	searchQuery := ""
	if exactSearch {
		var page int
		var err error
		searchQuery, page, limit, err = identityIPSearchRequest(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		offset = (page - 1) * limit
	} else if section == "ip-identities" {
		// Retain the legacy read-only contract for older clients; the admin UI uses the POST body path.
		searchQuery = r.URL.Query().Get("q")
	}
	switch section {
	case "identity-summary":
		result, err := s.identity.Summary(r.Context(), userID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	case "ip-identities":
		lookupKey, validSearch := identityIPSearchLookup(s.identity.protector, searchQuery)
		if !validSearch {
			writeJSON(w, http.StatusOK, identityPaged([]NetworkIdentityRow{}, 0, limit, offset))
			return
		}
		items, total, err := s.identity.repo.ListNetworks(r.Context(), userID, lookupKey, limit, offset)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		for index := range items {
			ip, err := s.identity.protector.DecryptIP(items[index].Ciphertext, items[index].Nonce, items[index].LookupKey, items[index].KeyID)
			if err != nil {
				writeError(w, http.StatusServiceUnavailable, errors.New("identity key unavailable"))
				return
			}
			items[index].IP = ip
		}
		writeJSON(w, http.StatusOK, identityPaged(items, total, limit, offset))
	case "device-identities":
		items, total, err := s.identity.repo.ListDevices(r.Context(), userID, limit, offset)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, identityPaged(items, total, limit, offset))
	case "associated-users":
		items, total, err := s.identity.repo.ListAssociatedUsers(r.Context(), userID, limit, offset)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, identityPaged(items, total, limit, offset))
	}
}

func identityIPSearchRequest(r *http.Request) (string, int, int, error) {
	var input struct {
		Query string `json:"query"`
		Page  int    `json:"page"`
		Limit int    `json:"limit"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return "", 0, 0, errors.New("invalid identity search request")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return "", 0, 0, errors.New("invalid identity search request")
	}
	input.Query = strings.TrimSpace(input.Query)
	if input.Query == "" || input.Page < 1 || input.Limit < 1 || input.Limit > 100 {
		return "", 0, 0, errors.New("invalid identity search request")
	}
	return input.Query, input.Page, input.Limit, nil
}

func identityIPSearchLookup(protector *IdentityProtector, raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", true
	}
	normalized, err := normalizeIdentityIP(raw)
	if err != nil || !normalized.Public {
		return "", false
	}
	return protector.LookupKey("ip", normalized.Value), true
}

func (s *HTTPServer) handleIdentitySummaries(w http.ResponseWriter, r *http.Request) {
	if s.identity == nil || !s.cfg.Identity.AdminEnabled {
		writeError(w, http.StatusServiceUnavailable, errors.New("identity admin is disabled"))
		return
	}
	userIDs := uniqueIdentityUserIDs(parseUserIDs(r.URL.Query().Get("user_ids")))
	if len(userIDs) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("user_ids is required"))
		return
	}
	items, err := s.identity.repo.ListSummaries(r.Context(), userIDs, s.cfg.Identity, s.identity.protector)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func uniqueIdentityUserIDs(input []int64) []int64 {
	seen := make(map[int64]struct{}, len(input))
	result := make([]int64, 0, min(len(input), 100))
	for _, userID := range input {
		if userID <= 0 || len(result) >= 100 {
			continue
		}
		if _, exists := seen[userID]; exists {
			continue
		}
		seen[userID] = struct{}{}
		result = append(result, userID)
	}
	return result
}

func identityPagination(r *http.Request) (int, int) {
	limit, offset := pagination(r)
	if limit > 100 {
		limit = 100
	}
	return limit, offset
}
func identityPaged(items any, total, limit, offset int) map[string]any {
	return map[string]any{"items": items, "total": total, "page": offset/limit + 1, "page_size": limit}
}

func (s *HTTPServer) handleIdentityHealth(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Identity.AdminEnabled {
		if s.identity != nil {
			health, err := s.identity.Health(r.Context())
			if err != nil {
				writeError(w, http.StatusServiceUnavailable, err)
				return
			}
			writeJSON(w, http.StatusOK, health)
			return
		}
		if s.cfg.Identity.active() {
			writeError(w, http.StatusServiceUnavailable, errors.New("identity service unavailable"))
			return
		}
		writeJSON(w, http.StatusOK, IdentityHealth{
			Enabled:      s.cfg.Identity.Enabled,
			AdminEnabled: false,
			Mode:         "shadow",
			Schema:       "v2",
			GeoSource:    s.cfg.Identity.GeoSource,
			Domains:      map[string]string{"ip": "disabled", "device": "disabled", "composite": "disabled"},
			Quality24H:   map[string]any{},
		})
		return
	}
	if s.identity == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("identity service unavailable"))
		return
	}
	health, err := s.identity.Health(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err)
		return
	}
	writeJSON(w, http.StatusOK, health)
}

func (s *HTTPServer) handleIdentityRules(w http.ResponseWriter, r *http.Request) {
	if s.identity == nil || !s.cfg.Identity.AdminEnabled {
		writeError(w, http.StatusServiceUnavailable, errors.New("identity admin is disabled"))
		return
	}
	rules, err := s.identity.Rules(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rules})
}

func (s *HTTPServer) handleIdentityRebuild(w http.ResponseWriter, r *http.Request, dryRun bool, body []byte) {
	if s.identity == nil || !s.cfg.Identity.AdminEnabled {
		writeError(w, http.StatusServiceUnavailable, errors.New("identity admin is disabled"))
		return
	}
	var input struct {
		ApprovedDryRunID int64 `json:"approved_dry_run_id"`
	}
	if err := decodeStrictJSON(body, &input); err != nil || dryRun && input.ApprovedDryRunID != 0 || !dryRun && input.ApprovedDryRunID <= 0 {
		writeError(w, http.StatusBadRequest, errors.New("历史回放写入必须使用当前管理员刚完成的有效预检"))
		return
	}
	actor, _ := actorID(r)
	result, err := s.identity.repo.Rebuild(r.Context(), actor, dryRun, input.ApprovedDryRunID, s.cfg.Identity)
	if err != nil {
		status := http.StatusInternalServerError
		if !dryRun {
			status = http.StatusConflict
		}
		_ = s.service.RecordAudit(r.Context(), AuditReport{ActorID: actor, Action: map[bool]string{true: "identity_rebuild_dry_run", false: "identity_rebuild"}[dryRun], TargetType: "identity", TargetID: "all", Result: "failed", Reason: err.Error()})
		writeError(w, status, err)
		return
	}
	_ = s.service.RecordAudit(r.Context(), AuditReport{ActorID: actor, Action: map[bool]string{true: "identity_rebuild_dry_run", false: "identity_rebuild"}[dryRun], TargetType: "identity", TargetID: "all", Result: "success", Metadata: map[string]any{"job_id": result.ID, "changed_subjects": result.ChangedSubjects}})
	writeJSON(w, http.StatusAccepted, result)
}

func (s *HTTPServer) handleIdentityRebuildStatus(w http.ResponseWriter, r *http.Request, path string) {
	if s.identity == nil || !s.cfg.Identity.AdminEnabled {
		writeError(w, http.StatusServiceUnavailable, errors.New("identity admin is disabled"))
		return
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(path, "/api/v1/admin/risk-rebuilds/"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, errors.New("invalid rebuild id"))
		return
	}
	result, err := s.identity.repo.GetRebuild(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, errors.New("rebuild not found"))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
