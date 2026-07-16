package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	ErrMissingSignature = errors.New("missing internal signature")
	ErrInvalidSignature = errors.New("invalid internal signature")
	ErrStaleSignature   = errors.New("stale internal signature")
	ErrReplaySignature  = errors.New("replayed internal signature")
)

type nonceStore struct {
	mu     sync.Mutex
	nonces map[string]time.Time
}

func newNonceStore() *nonceStore { return &nonceStore{nonces: make(map[string]time.Time)} }

func (n *nonceStore) use(value string, now time.Time) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	for key, created := range n.nonces {
		if now.Sub(created) > 10*time.Minute {
			delete(n.nonces, key)
		}
	}
	if _, exists := n.nonces[value]; exists {
		return false
	}
	n.nonces[value] = now
	return true
}

func verifyInternalSignature(r *http.Request, body []byte, secret string, nonces *nonceStore, now time.Time) error {
	if strings.TrimSpace(secret) == "" || r == nil {
		return ErrMissingSignature
	}
	timestampRaw := strings.TrimSpace(r.Header.Get("X-Risk-Timestamp"))
	nonce := strings.TrimSpace(r.Header.Get("X-Risk-Nonce"))
	signature := strings.TrimSpace(r.Header.Get("X-Risk-Signature"))
	if timestampRaw == "" || nonce == "" || signature == "" {
		return ErrMissingSignature
	}
	timestamp, err := strconv.ParseInt(timestampRaw, 10, 64)
	if err != nil || absInt64(now.Unix()-timestamp) > 300 {
		return ErrStaleSignature
	}
	hash := hmac.New(sha256.New, []byte(secret))
	_, _ = hash.Write([]byte(timestampRaw + "\n" + nonce + "\n"))
	_, _ = hash.Write(body)
	expected := hex.EncodeToString(hash.Sum(nil))
	provided, err := hex.DecodeString(signature)
	if err != nil || !hmac.Equal([]byte(expected), []byte(strings.ToLower(signature))) {
		return ErrInvalidSignature
	}
	_ = provided
	if nonces == nil || !nonces.use(nonce, now) {
		return ErrReplaySignature
	}
	return nil
}

func actorID(r *http.Request) (int64, error) {
	value := strings.TrimSpace(r.Header.Get("X-Risk-Actor-ID"))
	if value == "" {
		return 0, fmt.Errorf("missing actor id")
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid actor id")
	}
	return id, nil
}

func absInt64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}
