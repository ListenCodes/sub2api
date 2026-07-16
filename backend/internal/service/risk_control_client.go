package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	pathpkg "path"
	"strings"
	"time"
)

const maxHomepageProxyBody = 1 << 20

var (
	ErrInvalidHomepageRequest   = errors.New("invalid homepage request")
	ErrHomepageResponseTooLarge = errors.New("homepage response too large")
)

type HomepageAsset struct {
	Body         []byte
	Status       int
	ContentType  string
	CacheControl string
}

type RiskControlClient struct {
	baseURL string
	secret  []byte
	http    *http.Client
}

// RiskEventReport is the redacted event contract sent to the standalone risk service.
// Callers must provide hashed or coarse-grained correlation values; raw credentials,
// device identifiers, and request bodies are intentionally not part of this type.
type RiskEventReport struct {
	EventKey              string         `json:"event_key"`
	EventType             string         `json:"event_type"`
	UserID                int64          `json:"user_id,omitempty"`
	SubjectID             string         `json:"subject_id,omitempty"`
	UsernameSnapshot      string         `json:"username,omitempty"`
	AccountStatusSnapshot string         `json:"account_status,omitempty"`
	EmailHash             string         `json:"email_hash,omitempty"`
	IPHash                string         `json:"ip_hash,omitempty"`
	DeviceHash            string         `json:"device_hash,omitempty"`
	RiskType              string         `json:"risk_type,omitempty"`
	ErrorCode             string         `json:"error_code,omitempty"`
	Reason                string         `json:"reason,omitempty"`
	Endpoint              string         `json:"endpoint,omitempty"`
	Model                 string         `json:"model,omitempty"`
	HTTPStatus            int            `json:"http_status,omitempty"`
	OccurredAt            time.Time      `json:"occurred_at,omitempty"`
	Evidence              map[string]any `json:"evidence,omitempty"`
}

type RiskAuditReport struct {
	AuditKey   string         `json:"audit_key,omitempty"`
	ActorID    int64          `json:"actor_id"`
	Action     string         `json:"action"`
	TargetType string         `json:"target_type"`
	TargetID   string         `json:"target_id"`
	Result     string         `json:"result"`
	Reason     string         `json:"reason,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type RiskDecision struct {
	Action    string   `json:"decision"`
	Score     int      `json:"score"`
	RiskLevel string   `json:"risk_level"`
	Reason    string   `json:"reason"`
	EventID   int64    `json:"event_id,omitempty"`
	RuleCodes []string `json:"rule_codes,omitempty"`
	Mode      string   `json:"mode,omitempty"`
}

func NewRiskControlClientFromEnv() *RiskControlClient {
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("RISK_CONTROL_URL")), "/")
	secret := strings.TrimSpace(os.Getenv("RISK_CONTROL_INTERNAL_SECRET"))
	if baseURL == "" || secret == "" {
		return nil
	}
	return &RiskControlClient{baseURL: baseURL, secret: []byte(secret), http: &http.Client{Timeout: 800 * time.Millisecond}}
}

func (c *RiskControlClient) ProxyHomepage(ctx context.Context, method, assetPath string) (*HomepageAsset, error) {
	return c.proxyExtensionAsset(ctx, method, "/homepage", assetPath)
}

func (c *RiskControlClient) proxyExtensionAsset(ctx context.Context, method, prefix, assetPath string) (*HomepageAsset, error) {
	if c == nil || c.http == nil {
		return nil, fmt.Errorf("risk control client is not configured")
	}
	if method != http.MethodGet && method != http.MethodHead {
		return nil, ErrInvalidHomepageRequest
	}
	rawPath := strings.TrimSpace(assetPath)
	if strings.Contains(rawPath, `\`) {
		return nil, ErrInvalidHomepageRequest
	}
	relativePath := strings.TrimPrefix(rawPath, "/")
	upstreamPath := prefix + "/"
	if relativePath != "" {
		cleanPath := pathpkg.Clean("/" + relativePath)
		if cleanPath != "/"+relativePath || strings.Contains(relativePath, "..") {
			return nil, ErrInvalidHomepageRequest
		}
		upstreamPath = prefix + cleanPath
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+upstreamPath, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxHomepageProxyBody+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxHomepageProxyBody {
		return nil, ErrHomepageResponseTooLarge
	}
	return &HomepageAsset{
		Body:         body,
		Status:       resp.StatusCode,
		ContentType:  resp.Header.Get("Content-Type"),
		CacheControl: resp.Header.Get("Cache-Control"),
	}, nil
}

func HashRiskValue(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}

func (c *RiskControlClient) ReportEvent(ctx context.Context, input RiskEventReport) error {
	if c == nil {
		return nil
	}
	if input.OccurredAt.IsZero() {
		input.OccurredAt = time.Now().UTC()
	}
	body, err := json.Marshal(input)
	if err != nil {
		return err
	}
	_, err = c.postSigned(ctx, "/api/v1/internal/events", body)
	return err
}

func (c *RiskControlClient) EvaluateEvent(ctx context.Context, input RiskEventReport) (*RiskDecision, error) {
	if c == nil {
		return nil, nil
	}
	if input.OccurredAt.IsZero() {
		input.OccurredAt = time.Now().UTC()
	}
	body, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	responseBody, err := c.postSigned(ctx, "/api/v1/internal/events/evaluate", body)
	if err != nil {
		return nil, err
	}
	var decision RiskDecision
	if err := json.Unmarshal(responseBody, &decision); err != nil {
		return nil, fmt.Errorf("decode risk decision: %w", err)
	}
	return &decision, nil
}

func (c *RiskControlClient) ReportAudit(ctx context.Context, input RiskAuditReport) error {
	if c == nil {
		return nil
	}
	if strings.TrimSpace(input.AuditKey) == "" {
		input.AuditKey = stableAuditKey(input)
	}
	body, err := json.Marshal(input)
	if err != nil {
		return err
	}
	_, err = c.postSigned(ctx, "/api/v1/internal/audit", body)
	return err
}

func stableAuditKey(input RiskAuditReport) string {
	body, _ := json.Marshal(struct {
		ActorID    int64          `json:"actor_id"`
		Action     string         `json:"action"`
		TargetType string         `json:"target_type"`
		TargetID   string         `json:"target_id"`
		Result     string         `json:"result"`
		Reason     string         `json:"reason"`
		Metadata   map[string]any `json:"metadata"`
	}{input.ActorID, input.Action, input.TargetType, input.TargetID, input.Result, input.Reason, input.Metadata})
	sum := sha256.Sum256(body)
	return "audit:" + hex.EncodeToString(sum[:])
}

func (c *RiskControlClient) ProxyAdmin(ctx context.Context, method, path string, actorID int64, body []byte) ([]byte, int, error) {
	if c == nil || c.http == nil {
		return nil, 0, fmt.Errorf("risk control client is not configured")
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		responseBody, status, retryable, err := c.signedRequestOnce(ctx, method, path, actorID, body)
		if err == nil && status >= 200 && status < 300 {
			return responseBody, status, nil
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("risk control returned HTTP %d", status)
		}
		if !retryable || attempt == 2 {
			return responseBody, status, lastErr
		}
		backoff := time.Duration(25*(1<<attempt)) * time.Millisecond
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, 0, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, 0, lastErr
}

func (c *RiskControlClient) postSigned(ctx context.Context, path string, body []byte) ([]byte, error) {
	if c == nil || c.http == nil {
		return nil, fmt.Errorf("risk control client is not configured")
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		responseBody, status, retryable, err := c.signedRequestOnce(ctx, http.MethodPost, path, 0, body)
		if err == nil && status >= 200 && status < 300 {
			return responseBody, nil
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("risk control returned HTTP %d", status)
		}
		if !retryable || attempt == 2 {
			return nil, lastErr
		}
		backoff := time.Duration(25*(1<<attempt)) * time.Millisecond
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, lastErr
}

func (c *RiskControlClient) signedRequestOnce(ctx context.Context, method, path string, actorID int64, body []byte) ([]byte, int, bool, error) {
	nonce, err := randomToken(18)
	if err != nil {
		return nil, 0, false, err
	}
	ts := time.Now().Unix()
	mac := hmac.New(sha256.New, c.secret)
	_, _ = fmt.Fprintf(mac, "%d\n%s\n", ts, nonce)
	_, _ = mac.Write(body)
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, 0, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	if actorID > 0 {
		req.Header.Set("X-Risk-Actor-ID", fmt.Sprint(actorID))
	}
	req.Header.Set("X-Risk-Timestamp", fmt.Sprint(ts))
	req.Header.Set("X-Risk-Nonce", nonce)
	req.Header.Set("X-Risk-Signature", hex.EncodeToString(mac.Sum(nil)))
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, true, err
	}
	defer func() { _ = resp.Body.Close() }()
	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if readErr != nil {
		return nil, resp.StatusCode, resp.StatusCode >= 500, readErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return responseBody, resp.StatusCode, resp.StatusCode >= 500, fmt.Errorf("risk control returned %s: %s", resp.Status, strings.TrimSpace(string(responseBody)))
	}
	return responseBody, resp.StatusCode, false, nil
}

func randomToken(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
