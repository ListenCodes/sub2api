package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

type RiskControlClient struct {
	baseURL string
	secret  []byte
	http    *http.Client
}

type RiskRegistrationReport struct {
	RequestID string `json:"request_id"`
	SubjectID string `json:"subject_id"`
	Email string `json:"email"`
	IP string `json:"ip"`
	DeviceID string `json:"device_id"`
}

func NewRiskControlClientFromEnv() *RiskControlClient {
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("RISK_CONTROL_URL")), "/")
	secret := strings.TrimSpace(os.Getenv("RISK_CONTROL_INTERNAL_SECRET"))
	if baseURL == "" || secret == "" { return nil }
	return &RiskControlClient{baseURL: baseURL, secret: []byte(secret), http: &http.Client{Timeout: 120 * time.Millisecond}}
}

func (c *RiskControlClient) ReportRegistration(ctx context.Context, input RiskRegistrationReport) error {
	if c == nil { return nil }
	body, err := json.Marshal(input); if err != nil { return err }
	nonce, err := randomToken(18); if err != nil { return err }
	ts := time.Now().Unix()
	mac := hmac.New(sha256.New, c.secret)
	_, _ = mac.Write([]byte(fmt.Sprintf("%d\n%s\n", ts, nonce)))
	_, _ = mac.Write(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/internal/registration/decision", bytes.NewReader(body)); if err != nil { return err }
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Risk-Timestamp", fmt.Sprint(ts))
	req.Header.Set("X-Risk-Nonce", nonce)
	req.Header.Set("X-Risk-Signature", hex.EncodeToString(mac.Sum(nil)))
	resp, err := c.http.Do(req); if err != nil { return err }; defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 { return fmt.Errorf("risk control returned %s", resp.Status) }
	return nil
}

func randomToken(size int) (string, error) { buf := make([]byte, size); if _, err := rand.Read(buf); err != nil { return "", err }; return hex.EncodeToString(buf), nil }
