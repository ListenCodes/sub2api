package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func signIdentityRequest(t *testing.T, method, path string, body []byte, secret string, now time.Time) *http.Request {
	t.Helper()
	request := httptest.NewRequest(method, path, nil)
	timestamp := strconv.FormatInt(now.Unix(), 10)
	nonce := "test-nonce"
	digest := sha256.Sum256(body)
	canonical := "v2\n" + method + "\n" + path + "\n" + timestamp + "\n" + nonce + "\n" + hex.EncodeToString(digest[:])
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(canonical))
	request.Header.Set("X-Risk-Signature-Version", "v2")
	request.Header.Set("X-Risk-Timestamp", timestamp)
	request.Header.Set("X-Risk-Nonce", nonce)
	request.Header.Set("X-Risk-Signature", hex.EncodeToString(mac.Sum(nil)))
	return request
}

func TestIdentitySignatureBindsMethodPathAndBody(t *testing.T) {
	now := time.Now().UTC()
	body := []byte(`{"event_key":"one"}`)
	request := signIdentityRequest(t, http.MethodPost, "/api/v1/internal/identity-events", body, "01234567890123456789012345678901", now)
	if _, err := verifyIdentitySignature(request, body, "01234567890123456789012345678901", now); err != nil {
		t.Fatal(err)
	}
	request.Method = http.MethodPut
	if _, err := verifyIdentitySignature(request, body, "01234567890123456789012345678901", now); err == nil {
		t.Fatal("method mutation accepted")
	}
	request = signIdentityRequest(t, http.MethodPost, "/api/v1/internal/identity-events", body, "01234567890123456789012345678901", now)
	request.URL.Path = "/api/v1/internal/events"
	if _, err := verifyIdentitySignature(request, body, "01234567890123456789012345678901", now); err == nil {
		t.Fatal("path mutation accepted")
	}
}

func TestAdminV2SignatureBindsMethodPathAndActor(t *testing.T) {
	now := time.Now().UTC()
	body := []byte(`{"ok":true}`)
	request := signedAdminV2Request(http.MethodGet, "/api/v1/admin/users/9/ip-identities", body, testSecret, "7", "admin-v2-nonce", now)
	if err := verifyInternalSignature(request, body, testSecret, newNonceStore(), now); err != nil {
		t.Fatalf("valid admin signature: %v", err)
	}
	for name, mutate := range map[string]func(*http.Request){
		"path":  func(r *http.Request) { r.URL.Path = "/api/v1/admin/users/9/device-identities" },
		"query": func(r *http.Request) { r.URL.RawQuery = "page=2" },
		"actor": func(r *http.Request) { r.Header.Set("X-Risk-Actor-ID", "8") },
	} {
		t.Run(name, func(t *testing.T) {
			replayed := signedAdminV2Request(http.MethodGet, "/api/v1/admin/users/9/ip-identities", body, testSecret, "7", "admin-v2-"+name, now)
			mutate(replayed)
			if err := verifyInternalSignature(replayed, body, testSecret, newNonceStore(), now); !errors.Is(err, ErrInvalidSignature) {
				t.Fatalf("mutated request error = %v", err)
			}
		})
	}
}

func signedAdminV2Request(method, path string, body []byte, secret, actor, nonce string, now time.Time) *http.Request {
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	timestamp := strconv.FormatInt(now.Unix(), 10)
	digest := sha256.Sum256(body)
	canonical := fmt.Sprintf("admin-v2\n%s\n%s\n%s\n%s\n%s\n%s", method, path, actor, timestamp, nonce, hex.EncodeToString(digest[:]))
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(canonical))
	request.Header.Set("X-Risk-Signature-Version", "admin-v2")
	request.Header.Set("X-Risk-Actor-ID", actor)
	request.Header.Set("X-Risk-Timestamp", timestamp)
	request.Header.Set("X-Risk-Nonce", nonce)
	request.Header.Set("X-Risk-Signature", hex.EncodeToString(mac.Sum(nil)))
	return request
}
