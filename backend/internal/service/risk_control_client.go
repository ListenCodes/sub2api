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
	"sync/atomic"
	"time"
)

const (
	maxHomepageProxyBody  = 1 << 20
	riskIdentityQueueSize = 1024
	riskIdentityWorkers   = 2
	riskIdentityAttempts  = 3
	riskIdentityHeartbeat = 5 * time.Second
)

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
	baseURL                   string
	secret                    []byte
	http                      *http.Client
	identityEnabled           bool
	identityIPEnabled         bool
	identityDeviceEnabled     bool
	identityCompositeEnforce  bool
	identityDeliveryEnabled   bool
	identitySource            string
	identityGeneration        string
	identityStartedAt         time.Time
	identityQueue             chan RiskIdentityReport
	identityEnqueued          atomic.Uint64
	identitySucceeded         atomic.Uint64
	identityFailed            atomic.Uint64
	identityLatencyNanos      atomic.Uint64
	identityDropped           atomic.Uint64
	identityDeliveryReady     atomic.Bool
	identityHeartbeatSequence atomic.Uint64
	identityLastEventAt       atomic.Int64
	identityLastSuccessAt     atomic.Int64
	identityLastFailureAt     atomic.Int64
	identityLastDropAt        atomic.Int64
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

// RiskIdentityReport is the only internal contract allowed to carry raw identity
// material. It is sent exclusively over the V2 method/path-bound signed endpoint.
type RiskIdentityReport struct {
	EventKey            string    `json:"event_key"`
	EventType           string    `json:"event_type"`
	EventClass          string    `json:"event_class"`
	Outcome             string    `json:"outcome"`
	OccurredAt          time.Time `json:"occurred_at"`
	UserID              int64     `json:"user_id,omitempty"`
	Email               string    `json:"email,omitempty"`
	ClientIP            string    `json:"client_ip,omitempty"`
	IPSource            string    `json:"ip_source,omitempty"`
	ProxyChainValid     bool      `json:"proxy_chain_valid"`
	CountryCode         string    `json:"country_code,omitempty"`
	Region              string    `json:"region,omitempty"`
	City                string    `json:"city,omitempty"`
	ASN                 int64     `json:"asn,omitempty"`
	GeoSource           string    `json:"geo_source,omitempty"`
	GeoVerified         bool      `json:"geo_verified"`
	BrowserInstanceID   string    `json:"browser_instance_id,omitempty"`
	BrowserCookieStatus string    `json:"browser_cookie_status,omitempty"`
	BrowserFamily       string    `json:"browser_family,omitempty"`
	OSFamily            string    `json:"os_family,omitempty"`
	DeviceClass         string    `json:"device_class,omitempty"`
	LanguageFamily      string    `json:"language_family,omitempty"`
	APIKeyID            int64     `json:"api_key_id,omitempty"`
}

type RiskIdentityDeliveryReport struct {
	Source        string `json:"source"`
	Generation    string `json:"generation"`
	StartedAt     string `json:"started_at"`
	Sequence      uint64 `json:"sequence"`
	Enqueued      uint64 `json:"enqueued"`
	Succeeded     uint64 `json:"succeeded"`
	Failed        uint64 `json:"failed"`
	Dropped       uint64 `json:"dropped"`
	QueueDepth    int    `json:"queue_depth"`
	LastEventAt   string `json:"last_event_at,omitempty"`
	LastSuccessAt string `json:"last_success_at,omitempty"`
	LastFailureAt string `json:"last_failure_at,omitempty"`
	LastDropAt    string `json:"last_drop_at,omitempty"`
	GeneratedAt   string `json:"generated_at"`
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
	client := &RiskControlClient{baseURL: baseURL, secret: []byte(secret), http: &http.Client{Timeout: 800 * time.Millisecond}}
	client.identityEnabled = envBoolValue("RISK_IDENTITY_V2_ENABLED")
	client.identityIPEnabled = envBoolValue("RISK_IDENTITY_IP_COLLECTION_ENABLED")
	client.identityDeviceEnabled = envBoolValue("RISK_IDENTITY_DEVICE_COLLECTION_ENABLED")
	client.identityCompositeEnforce = envBoolValue("RISK_IDENTITY_COMPOSITE_ENFORCEMENT_ENABLED")
	client.identityDeliveryEnabled = envBoolValue("RISK_IDENTITY_DELIVERY_ENABLED")
	client.identitySource = strings.TrimSpace(os.Getenv("RISK_IDENTITY_DELIVERY_SOURCE"))
	if client.identitySource == "" {
		client.identitySource = "sub2api"
	}
	client.identityGeneration, _ = randomToken(16)
	if client.identityGeneration == "" {
		client.identityGeneration = fmt.Sprintf("startup-%x", time.Now().UTC().UnixNano())
	}
	client.identityStartedAt = time.Now().UTC()
	if client.identityEnabled {
		client.identityQueue = make(chan RiskIdentityReport, riskIdentityQueueSize)
		for range riskIdentityWorkers {
			go client.runIdentityWorker()
		}
		if client.identityDeliveryEnabled {
			go client.runIdentityHeartbeat()
		}
	}
	return client
}

func envBoolValue(key string) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func (c *RiskControlClient) IdentityEnabled() bool { return c != nil && c.identityEnabled }
func (c *RiskControlClient) IdentityIPEnabled() bool {
	return c != nil && c.identityEnabled && c.identityIPEnabled
}
func (c *RiskControlClient) IdentityDeviceEnabled() bool {
	return c != nil && c.identityEnabled && c.identityDeviceEnabled
}
func (c *RiskControlClient) IdentityCompositeEnforcementEnabled() bool {
	return c != nil && c.identityEnabled && c.identityIPEnabled && c.identityDeviceEnabled && c.identityDeliveryEnabled && c.identityCompositeEnforce
}

func (c *RiskControlClient) EnqueueIdentity(input RiskIdentityReport) bool {
	if c == nil || !c.identityEnabled || c.identityQueue == nil {
		return false
	}
	if input.OccurredAt.IsZero() {
		input.OccurredAt = time.Now().UTC()
	}
	c.identityLastEventAt.Store(input.OccurredAt.UTC().UnixNano())
	select {
	case c.identityQueue <- input:
		c.identityEnqueued.Add(1)
		return true
	default:
		c.identityDropped.Add(1)
		c.identityLastDropAt.Store(time.Now().UTC().UnixNano())
		return false
	}
}

func (c *RiskControlClient) IdentityDropped() uint64 {
	if c == nil {
		return 0
	}
	return c.identityDropped.Load()
}

func (c *RiskControlClient) IdentityQueueHealth() map[string]any {
	if c == nil || !c.identityEnabled || c.identityQueue == nil {
		return map[string]any{"state": "disabled", "dropped": uint64(0)}
	}
	state := "healthy"
	if len(c.identityQueue) == cap(c.identityQueue) || c.identityDropped.Load() > 0 || c.identityFailed.Load() > 0 {
		state = "degraded"
	}
	processed := c.identitySucceeded.Load() + c.identityFailed.Load()
	averageLatencyMS := float64(0)
	if processed > 0 {
		averageLatencyMS = float64(c.identityLatencyNanos.Load()) / float64(processed) / float64(time.Millisecond)
	}
	return map[string]any{"state": state, "queued": len(c.identityQueue), "capacity": cap(c.identityQueue), "enqueued": c.identityEnqueued.Load(), "succeeded": c.identitySucceeded.Load(), "failed": c.identityFailed.Load(), "dropped": c.identityDropped.Load(), "average_latency_ms": averageLatencyMS, "last_event_at": atomicTime(c.identityLastEventAt.Load()), "last_success_at": atomicTime(c.identityLastSuccessAt.Load()), "last_failure_at": atomicTime(c.identityLastFailureAt.Load()), "last_drop_at": atomicTime(c.identityLastDropAt.Load())}
}

func (c *RiskControlClient) runIdentityWorker() {
	for input := range c.identityQueue {
		body, err := json.Marshal(input)
		if err != nil {
			c.identityFailed.Add(1)
			c.identityLastFailureAt.Store(time.Now().UTC().UnixNano())
			continue
		}
		started := time.Now()
		var status int
		var requestErr error
		for attempt := 0; attempt < riskIdentityAttempts; attempt++ {
			if c.identityDeliveryEnabled && !c.identityDeliveryReady.Load() {
				ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
				requestErr = c.sendIdentityHeartbeat(ctx)
				cancel()
				if requestErr != nil {
					if attempt < riskIdentityAttempts-1 {
						time.Sleep(time.Duration(25*(1<<attempt)) * time.Millisecond)
					}
					continue
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
			_, nextStatus, retryable, nextErr := c.identitySignedRequestOnce(ctx, http.MethodPost, "/api/v1/internal/identity-events", body)
			cancel()
			status, requestErr = nextStatus, nextErr
			if requestErr == nil && status >= 200 && status < 300 {
				break
			}
			if !retryable || attempt == riskIdentityAttempts-1 {
				break
			}
			time.Sleep(time.Duration(25*(1<<attempt)) * time.Millisecond)
		}
		c.identityLatencyNanos.Add(uint64(time.Since(started)))
		if requestErr == nil && status >= 200 && status < 300 {
			c.identitySucceeded.Add(1)
			c.identityLastSuccessAt.Store(time.Now().UTC().UnixNano())
		} else {
			c.identityFailed.Add(1)
			c.identityLastFailureAt.Store(time.Now().UTC().UnixNano())
		}
	}
}

func (c *RiskControlClient) runIdentityHeartbeat() {
	ticker := time.NewTicker(riskIdentityHeartbeat)
	defer ticker.Stop()
	for {
		if c == nil || !c.identityDeliveryEnabled || c.identityQueue == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
		err := c.sendIdentityHeartbeat(ctx)
		cancel()
		if err != nil {
			c.identityDeliveryReady.Store(false)
		}
		<-ticker.C
	}
}

func (c *RiskControlClient) sendIdentityHeartbeat(ctx context.Context) error {
	if c == nil || !c.identityDeliveryEnabled || c.identityQueue == nil {
		return errors.New("identity delivery is disabled")
	}
	report := RiskIdentityDeliveryReport{
		Source: c.identitySource, Generation: c.identityGeneration, StartedAt: c.identityStartedAt.Format(time.RFC3339Nano), Sequence: c.identityHeartbeatSequence.Add(1), Enqueued: c.identityEnqueued.Load(),
		Succeeded: c.identitySucceeded.Load(), Failed: c.identityFailed.Load(), Dropped: c.identityDropped.Load(),
		QueueDepth: len(c.identityQueue), LastEventAt: atomicTime(c.identityLastEventAt.Load()), LastSuccessAt: atomicTime(c.identityLastSuccessAt.Load()),
		LastFailureAt: atomicTime(c.identityLastFailureAt.Load()), LastDropAt: atomicTime(c.identityLastDropAt.Load()), GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	body, err := json.Marshal(report)
	if err != nil {
		return err
	}
	_, status, _, err := c.identitySignedRequestOnce(ctx, http.MethodPost, "/api/v1/internal/identity-delivery", body)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("identity delivery heartbeat returned status %d", status)
	}
	c.identityDeliveryReady.Store(true)
	return nil
}

func atomicTime(value int64) string {
	if value <= 0 {
		return ""
	}
	return time.Unix(0, value).UTC().Format(time.RFC3339Nano)
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

func (c *RiskControlClient) EvaluateIdentityRegistration(ctx context.Context, input RiskIdentityReport) (*RiskDecision, error) {
	if c == nil || !c.IdentityCompositeEnforcementEnabled() {
		return nil, nil
	}
	if input.OccurredAt.IsZero() {
		input.OccurredAt = time.Now().UTC()
	}
	body, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	responseBody, _, _, err := c.identitySignedRequestOnce(ctx, http.MethodPost, "/api/v1/internal/identity-registration-decision", body)
	if err != nil {
		return nil, err
	}
	var decision RiskDecision
	if err := json.Unmarshal(responseBody, &decision); err != nil {
		return nil, fmt.Errorf("decode identity registration decision: %w", err)
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
	actor := fmt.Sprint(actorID)
	bodyDigest := sha256.Sum256(body)
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, 0, false, err
	}
	canonical := fmt.Sprintf("admin-v2\n%s\n%s\n%s\n%d\n%s\n%s", strings.ToUpper(method), req.URL.RequestURI(), actor, ts, nonce, hex.EncodeToString(bodyDigest[:]))
	mac := hmac.New(sha256.New, c.secret)
	_, _ = mac.Write([]byte(canonical))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Risk-Signature-Version", "admin-v2")
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

func (c *RiskControlClient) identitySignedRequestOnce(ctx context.Context, method, path string, body []byte) ([]byte, int, bool, error) {
	if c == nil || c.http == nil || !c.identityEnabled {
		return nil, 0, false, errors.New("risk identity client is not configured")
	}
	nonce, err := randomToken(18)
	if err != nil {
		return nil, 0, false, err
	}
	ts := time.Now().Unix()
	bodyDigest := sha256.Sum256(body)
	canonical := fmt.Sprintf("v2\n%s\n%s\n%d\n%s\n%s", strings.ToUpper(method), path, ts, nonce, hex.EncodeToString(bodyDigest[:]))
	mac := hmac.New(sha256.New, c.secret)
	_, _ = mac.Write([]byte(canonical))
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, 0, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Risk-Signature-Version", "v2")
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
		return responseBody, resp.StatusCode, resp.StatusCode >= 500, fmt.Errorf("risk identity returned HTTP %d", resp.StatusCode)
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
