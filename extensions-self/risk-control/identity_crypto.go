package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

type IdentityProtector struct {
	hmacKey       []byte
	aead          cipher.AEAD
	keyID         string
	previousAEAD  cipher.AEAD
	previousKeyID string
}

func NewIdentityProtector(cfg IdentityConfig) (*IdentityProtector, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if !cfg.active() {
		return nil, nil
	}
	hmacKey, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(cfg.HMACKey))
	encryptionKey, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(cfg.EncryptionKey))
	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	protector := &IdentityProtector{hmacKey: hmacKey, aead: aead, keyID: strings.TrimSpace(cfg.EncryptionKeyID)}
	if strings.TrimSpace(cfg.PreviousEncryptionKey) != "" {
		previousKey, err := base64.StdEncoding.DecodeString(strings.TrimSpace(cfg.PreviousEncryptionKey))
		if err != nil || len(previousKey) != 32 {
			return nil, ErrIdentityKeyLength
		}
		previousBlock, err := aes.NewCipher(previousKey)
		if err != nil {
			return nil, err
		}
		protector.previousAEAD, err = cipher.NewGCM(previousBlock)
		if err != nil {
			return nil, err
		}
		protector.previousKeyID = strings.TrimSpace(cfg.PreviousEncryptionKeyID)
	}
	return protector, nil
}

func (p *IdentityProtector) LookupKey(kind, value string) string {
	if p == nil || len(p.hmacKey) == 0 || strings.TrimSpace(value) == "" {
		return ""
	}
	hash := hmac.New(sha256.New, p.hmacKey)
	_, _ = hash.Write([]byte("risk-identity-v2\n" + strings.TrimSpace(kind) + "\n" + strings.TrimSpace(value)))
	return hex.EncodeToString(hash.Sum(nil))
}

func (p *IdentityProtector) EncryptIP(normalizedIP, lookupKey string) ([]byte, []byte, error) {
	if p == nil || p.aead == nil || normalizedIP == "" || lookupKey == "" {
		return nil, nil, errors.New("identity encryption unavailable")
	}
	nonce := make([]byte, p.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	aad := []byte(fmt.Sprintf("risk-identity-v2\nip\n%s\n%s", p.keyID, lookupKey))
	return p.aead.Seal(nil, nonce, []byte(normalizedIP), aad), nonce, nil
}

func (p *IdentityProtector) DecryptIP(ciphertext, nonce []byte, lookupKey, keyID string) (string, error) {
	if p == nil || p.aead == nil {
		return "", errors.New("identity encryption key unavailable")
	}
	aead := p.aead
	if keyID != p.keyID {
		if p.previousAEAD == nil || keyID != p.previousKeyID {
			return "", errors.New("identity encryption key unavailable")
		}
		aead = p.previousAEAD
	}
	aad := []byte(fmt.Sprintf("risk-identity-v2\nip\n%s\n%s", keyID, lookupKey))
	plaintext, err := aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

type normalizedIP struct {
	Value       string
	PrefixValue string
	Family      int
	Public      bool
}

func normalizeIdentityIP(raw string) (normalizedIP, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) == 0 || len(raw) > 64 {
		return normalizedIP{}, errors.New("invalid client_ip")
	}
	address, err := netip.ParseAddr(raw)
	if err != nil {
		return normalizedIP{}, errors.New("invalid client_ip")
	}
	address = address.Unmap()
	prefixBits := 64
	family := 6
	if address.Is4() {
		prefixBits = 24
		family = 4
	}
	prefix := netip.PrefixFrom(address, prefixBits).Masked()
	public := address.IsGlobalUnicast() && !address.IsPrivate() && !address.IsLoopback() && !address.IsLinkLocalUnicast()
	return normalizedIP{Value: address.String(), PrefixValue: prefix.String(), Family: family, Public: public}, nil
}
