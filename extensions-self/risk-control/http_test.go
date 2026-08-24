package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

const testSecret = "risk-test-secret"

func TestInternalEventRequiresFreshSignedRequest(t *testing.T) {
	server := NewHTTPServer(Config{InternalSecret: testSecret, Mode: "enforce"}, NewMemoryRepository(defaultRules()))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/internal/events/evaluate", bytes.NewBufferString(`{"event_key":"x","event_type":"login_failure"}`))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
}

func TestInternalEventIsIdempotentAndReturnsDecision(t *testing.T) {
	server := NewHTTPServer(Config{InternalSecret: testSecret, Mode: "enforce"}, NewMemoryRepository(defaultRules()))
	body := []byte(`{"event_key":"login-42-1","event_type":"login_failure","user_id":42,"reason":"invalid credentials"}`)
	first := signedRequest(http.MethodPost, "/api/v1/internal/events/evaluate", body, testSecret, "nonce-1", time.Now())
	firstResponse := serveJSON(server, first)
	if firstResponse.Code != http.StatusOK {
		t.Fatalf("first status = %d, body = %s", firstResponse.Code, firstResponse.Body.String())
	}
	second := signedRequest(http.MethodPost, "/api/v1/internal/events/evaluate", body, testSecret, "nonce-2", time.Now())
	secondResponse := serveJSON(server, second)
	if secondResponse.Code != http.StatusOK {
		t.Fatalf("second status = %d, body = %s", secondResponse.Code, secondResponse.Body.String())
	}
	var firstDecision, secondDecision Decision
	decodeBody(t, firstResponse, &firstDecision)
	decodeBody(t, secondResponse, &secondDecision)
	if firstDecision.EventID == 0 || firstDecision.EventID != secondDecision.EventID {
		t.Fatalf("decisions = %+v / %+v", firstDecision, secondDecision)
	}
	subjects, _, err := server.repo.ListSubjects(context.Background(), 20, 0, "", "", nil)
	if err != nil {
		t.Fatalf("ListSubjects() error = %v", err)
	}
	if len(subjects) != 1 || subjects[0].EventCount != 1 {
		t.Fatalf("subjects = %+v", subjects)
	}
}

func TestAdminRuleUpdateRequiresExpectedRevision(t *testing.T) {
	server := NewHTTPServer(Config{InternalSecret: testSecret, Mode: "enforce"}, NewMemoryRepository(defaultRules()))
	body := []byte(`{"enabled":false,"window_seconds":600,"threshold":5,"score":30,"risk_level":"medium","action":"review","revision":99,"reason":"verify optimistic locking"}`)
	request := signedRequest(http.MethodPut, "/api/v1/admin/rules/login_failure_burst", body, testSecret, "nonce-admin-1", time.Now())
	request.Header.Set("X-Risk-Actor-ID", "7")
	response := serveJSON(server, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestAdminRuleUpdateRejectsAmbiguousStrategySemantics(t *testing.T) {
	server := NewHTTPServer(Config{InternalSecret: testSecret, Mode: "enforce"}, NewMemoryRepository(defaultRules()))
	body := []byte(`{"name":"Login failures","event_types":["login_failure"],"count_strategy":"ip_distinct_success_users","enabled":true,"window_seconds":600,"threshold":5,"score":70,"risk_level":"high","action":"review","revision":1,"reason":"verify invalid strategy"}`)
	request := signedRequest(http.MethodPut, "/api/v1/admin/rules/login_failure_burst", body, testSecret, "nonce-admin-strategy", time.Now())
	request.Header.Set("X-Risk-Actor-ID", "7")
	response := serveJSON(server, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestAdminRuleTestTreatsSubmittedRuleAsEnabled(t *testing.T) {
	server := NewHTTPServer(Config{InternalSecret: testSecret, Mode: "enforce"}, NewMemoryRepository(nil))
	body := []byte(`{"sample":{"event_type":"login_failure","observed_count":5,"user_id":42},"rule":{"code":"login_failure","event_types":["login_failure"],"count_strategy":"user_events","threshold":5,"score":80,"risk_level":"high","action":"review"}}`)
	request := signedRequest(http.MethodPost, "/api/v1/admin/rules/test", body, testSecret, "nonce-rule-test", time.Now())
	request.Header.Set("X-Risk-Actor-ID", "7")
	response := serveJSON(server, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload struct {
		Matched bool `json:"matched"`
	}
	decodeBody(t, response, &payload)
	if !payload.Matched {
		t.Fatalf("rule test payload = %s, want matched", response.Body.String())
	}
}

func TestAdminRuleCreatePersistsSubmittedCountStrategyContract(t *testing.T) {
	repo := NewMemoryRepository(nil)
	server := NewHTTPServer(Config{InternalSecret: testSecret, Mode: "enforce"}, repo)
	body := []byte(`{"code":"registration_email_retries","name":"Registration email retries","event_types":["registration_attempt"],"count_strategy":"email_subject_events","enabled":true,"window_seconds":600,"threshold":5,"score":0,"risk_level":"low","action":"observe","reason":"create explicit registration counter"}`)
	request := signedRequest(http.MethodPost, "/api/v1/admin/rules", body, testSecret, "nonce-create-count-strategy", time.Now())
	request.Header.Set("X-Risk-Actor-ID", "7")
	response := serveJSON(server, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var created Rule
	decodeBody(t, response, &created)
	if created.CountStrategy != countStrategyEmailSubjectEvents {
		t.Fatalf("response count_strategy = %q", created.CountStrategy)
	}
	rules, err := repo.ListRules(context.Background())
	if err != nil || len(rules) != 1 || rules[0].CountStrategy != countStrategyEmailSubjectEvents {
		t.Fatalf("persisted rules = %+v, error = %v", rules, err)
	}
}

func TestAdminAuditIsIdempotentByAuditKey(t *testing.T) {
	repo := NewMemoryRepository(nil)
	server := NewHTTPServer(Config{InternalSecret: testSecret, Mode: "enforce"}, repo)
	body := []byte(`{"audit_key":"status-42-operation-1","action":"ban","target_type":"user","target_id":"42","result":"success"}`)
	for _, nonce := range []string{"nonce-audit-1", "nonce-audit-2"} {
		request := signedRequest(http.MethodPost, "/api/v1/internal/audit", body, testSecret, nonce, time.Now())
		response := serveJSON(server, request)
		if response.Code != http.StatusAccepted {
			t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
		}
	}
	items, total, err := repo.ListAudit(context.Background(), 10, 0, "", 0, "")
	if err != nil {
		t.Fatalf("ListAudit() error = %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("audit records = total %d items %+v, want one record", total, items)
	}
}

func TestAdminAuditSupportsPagination(t *testing.T) {
	repo := NewMemoryRepository(nil)
	for i := 1; i <= 3; i++ {
		if err := repo.InsertAudit(context.Background(), AuditRecord{AuditKey: formatUserID(int64(i)), Action: "ban", TargetType: "user", TargetID: formatUserID(int64(i)), Result: "success"}); err != nil {
			t.Fatalf("InsertAudit() error = %v", err)
		}
	}
	server := NewHTTPServer(Config{InternalSecret: testSecret, Mode: "enforce"}, repo)
	request := signedRequest(http.MethodGet, "/api/v1/admin/audit?limit=1&page=2", nil, testSecret, "nonce-audit-page", time.Now())
	request.Header.Set("X-Risk-Actor-ID", "7")
	response := serveJSON(server, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload struct {
		Items []AuditRecord `json:"items"`
		Total int           `json:"total"`
		Page  int           `json:"page"`
	}
	decodeBody(t, response, &payload)
	if payload.Total != 3 || payload.Page != 2 || len(payload.Items) != 1 {
		t.Fatalf("paginated payload = %+v", payload)
	}
}

func TestAdminAuditSupportsActorFilterAndTargetSort(t *testing.T) {
	repo := NewMemoryRepository(nil)
	for _, audit := range []AuditRecord{
		{ActorID: 7, Action: "ban", TargetType: "user", TargetID: "42", Result: "failed", CreatedAt: "2026-07-12T00:02:00Z"},
		{ActorID: 7, Action: "ban", TargetType: "user", TargetID: "7", Result: "success", CreatedAt: "2026-07-12T00:01:00Z"},
		{ActorID: 9, Action: "ban", TargetType: "user", TargetID: "1", Result: "success", CreatedAt: "2026-07-12T00:00:00Z"},
	} {
		if err := repo.InsertAudit(context.Background(), audit); err != nil {
			t.Fatalf("InsertAudit() error = %v", err)
		}
	}
	server := NewHTTPServer(Config{InternalSecret: testSecret, Mode: "enforce"}, repo)
	request := signedRequest(http.MethodGet, "/api/v1/admin/audit?actor_id=7&sort_by=target&sort_order=asc", nil, testSecret, "nonce-audit-filter", time.Now())
	request.Header.Set("X-Risk-Actor-ID", "7")
	response := serveJSON(server, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload struct {
		Items []AuditRecord `json:"items"`
		Total int           `json:"total"`
	}
	decodeBody(t, response, &payload)
	if payload.Total != 2 || len(payload.Items) != 2 || payload.Items[0].TargetID != "7" || payload.Items[1].TargetID != "42" {
		t.Fatalf("filtered/sorted audit = %+v", payload)
	}
}

func TestAdminAuditSeparatesSensitiveRecordsFromDefaultCategory(t *testing.T) {
	repo := NewMemoryRepository(nil)
	for _, audit := range []AuditRecord{
		{ActorID: 7, Action: "ban", TargetType: "user", TargetID: "42", Result: "success"},
		{ActorID: 7, Action: "view_identity_detail", TargetType: "user", TargetID: "42", Result: "success", Metadata: map[string]any{"section": "associated-users"}},
	} {
		if err := repo.InsertAudit(context.Background(), audit); err != nil {
			t.Fatal(err)
		}
	}
	server := NewHTTPServer(Config{InternalSecret: testSecret, Mode: "shadow"}, repo)

	request := signedRequest(http.MethodGet, "/api/v1/admin/audit", nil, testSecret, "nonce-audit-default-category", time.Now())
	request.Header.Set("X-Risk-Actor-ID", "7")
	response := serveJSON(server, request)
	var defaultPayload struct {
		Items []AuditRecord `json:"items"`
		Total int           `json:"total"`
	}
	decodeBody(t, response, &defaultPayload)
	if response.Code != http.StatusOK || defaultPayload.Total != 1 || defaultPayload.Items[0].Action != "ban" {
		t.Fatalf("default category response = %d %s", response.Code, response.Body.String())
	}

	request = signedRequest(http.MethodGet, "/api/v1/admin/audit?category=sensitive", nil, testSecret, "nonce-audit-sensitive-category", time.Now())
	request.Header.Set("X-Risk-Actor-ID", "7")
	response = serveJSON(server, request)
	var sensitivePayload struct {
		Items []AuditRecord `json:"items"`
		Total int           `json:"total"`
	}
	decodeBody(t, response, &sensitivePayload)
	if response.Code != http.StatusOK || sensitivePayload.Total != 1 || sensitivePayload.Items[0].Action != "view_identity_detail" {
		t.Fatalf("sensitive category response = %d %s", response.Code, response.Body.String())
	}

	request = signedRequest(http.MethodGet, "/api/v1/admin/audit?category=unknown", nil, testSecret, "nonce-audit-invalid-category", time.Now())
	request.Header.Set("X-Risk-Actor-ID", "7")
	if response = serveJSON(server, request); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid category status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestSensitiveIdentityAuditMergesSectionsWithinOneViewSession(t *testing.T) {
	repo := NewMemoryRepository(nil)
	for _, audit := range []AuditRecord{
		{AuditKey: "identity-view:session-1", ActorID: 7, Action: "view_identity_detail", TargetType: "user", TargetID: "42", Result: "success", Metadata: map[string]any{"section": "identity-summary", "sections": []string{"identity-summary"}}},
		{AuditKey: "identity-view:session-1", ActorID: 7, Action: "view_identity_detail", TargetType: "user", TargetID: "42", Result: "success", Metadata: map[string]any{"section": "ip-identities", "sections": []string{"ip-identities"}}},
	} {
		if err := repo.InsertAudit(context.Background(), audit); err != nil {
			t.Fatal(err)
		}
	}
	items, total, err := repo.ListAudit(context.Background(), 20, 0, "view_identity_detail", 42, "")
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("session audit count = %d, items = %+v", total, items)
	}
	sections, ok := items[0].Metadata["sections"].([]string)
	if !ok || len(sections) != 2 || sections[0] != "identity-summary" || sections[1] != "ip-identities" {
		t.Fatalf("merged sections = %#v", items[0].Metadata["sections"])
	}
}

func TestAdminUsersAppliesRiskFiltersBeforePagination(t *testing.T) {
	repo := NewMemoryRepository(nil)
	if err := repo.UpsertSubject(context.Background(), EventRecord{
		UserID: 1, UsernameSnapshot: "lower-score", RiskType: "api_error",
		RiskLevel: "medium", Score: 90, Decision: "review", OccurredAt: "2026-07-12T00:00:00Z",
	}); err != nil {
		t.Fatalf("seed medium subject: %v", err)
	}
	if err := repo.UpsertSubject(context.Background(), EventRecord{
		UserID: 2, UsernameSnapshot: "high-risk", RiskType: "content_risk",
		RiskLevel: "high", Score: 80, Decision: "review", OccurredAt: "2026-07-12T00:01:00Z",
	}); err != nil {
		t.Fatalf("seed high subject: %v", err)
	}

	server := NewHTTPServer(Config{InternalSecret: testSecret, Mode: "enforce"}, repo)
	request := signedRequest(http.MethodGet, "/api/v1/admin/users?risk_level=high&limit=1", nil, testSecret, "nonce-users-filter", time.Now())
	request.Header.Set("X-Risk-Actor-ID", "7")
	response := serveJSON(server, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload struct {
		Items []struct {
			ID        int64  `json:"id"`
			RiskLevel string `json:"risk_level"`
		} `json:"items"`
		Total int `json:"total"`
	}
	decodeBody(t, response, &payload)
	if payload.Total != 1 || len(payload.Items) != 1 || payload.Items[0].ID != 2 || payload.Items[0].RiskLevel != "high" {
		t.Fatalf("filtered users = %+v, want only high-risk user 2", payload)
	}
}

func TestAdminMarkUserProcessedClearsPendingAndWritesAudit(t *testing.T) {
	repo := NewMemoryRepository(nil)
	if err := repo.UpsertSubject(context.Background(), EventRecord{UserID: 42, RiskType: "login_failure", RiskLevel: "high", Score: 80, Decision: "review", OccurredAt: "2026-07-12T00:00:00Z"}); err != nil {
		t.Fatalf("seed subject: %v", err)
	}
	server := NewHTTPServer(Config{InternalSecret: testSecret, Mode: "enforce"}, repo)
	body := []byte(`{"reason":"人工复核完成","batch_id":"batch-42"}`)
	request := signedRequest(http.MethodPost, "/api/v1/admin/users/42/processed", body, testSecret, "nonce-processed", time.Now())
	request.Header.Set("X-Risk-Actor-ID", "7")
	response := serveJSON(server, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	subject, found, err := repo.GetSubject(context.Background(), 42)
	if err != nil || !found || subject.Pending {
		t.Fatalf("subject = %+v, found=%v, error=%v", subject, found, err)
	}
	items, total, err := repo.ListAudit(context.Background(), 10, 0, "mark_processed", 42, "")
	if err != nil || total != 1 || len(items) != 1 || items[0].ActorID != 7 || items[0].Reason != "人工复核完成" || items[0].AuditKey != "batch-42:42" {
		t.Fatalf("audit = total %d items %+v error=%v", total, items, err)
	}
}

func signedRequest(method, path string, body []byte, secret, nonce string, now time.Time) *http.Request {
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	timestamp := strconv.FormatInt(now.Unix(), 10)
	hash := hmac.New(sha256.New, []byte(secret))
	_, _ = hash.Write([]byte(timestamp + "\n" + nonce + "\n"))
	_, _ = hash.Write(body)
	request.Header.Set("X-Risk-Timestamp", timestamp)
	request.Header.Set("X-Risk-Nonce", nonce)
	request.Header.Set("X-Risk-Signature", hex.EncodeToString(hash.Sum(nil)))
	request.Header.Set("Content-Type", "application/json")
	return request
}

func serveJSON(server http.Handler, request *http.Request) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	return response
}

func decodeBody(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
		t.Fatalf("decode response %q: %v", response.Body.String(), err)
	}
}

func TestSignatureHelperUsesExpectedBody(t *testing.T) {
	request := signedRequest(http.MethodPost, "/", []byte(`{"a":1}`), testSecret, "nonce", time.Now())
	body, _ := io.ReadAll(request.Body)
	request.Body = io.NopCloser(bytes.NewReader(body))
	if request.Header.Get("X-Risk-Signature") == "" {
		t.Fatal("signature missing")
	}
}
