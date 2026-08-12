package main

import (
	"encoding/base64"
	"testing"
)

func testIdentityConfig() IdentityConfig {
	return IdentityConfig{Enabled: true, HMACKey: base64.StdEncoding.EncodeToString([]byte("12345678901234567890123456789012")), EncryptionKey: base64.StdEncoding.EncodeToString([]byte("abcdefghijklmnopqrstuvwxyzABCDEF")), EncryptionKeyID: "test-key"}
}

func TestIdentityProtectorSeparatesLookupDomainsAndBindsCiphertext(t *testing.T) {
	protector, err := NewIdentityProtector(testIdentityConfig())
	if err != nil {
		t.Fatal(err)
	}
	ipKey := protector.LookupKey("ip", "203.0.113.7")
	if ipKey == protector.LookupKey("email", "203.0.113.7") {
		t.Fatal("lookup keys must be domain separated")
	}
	ciphertext, nonce, err := protector.EncryptIP("203.0.113.7", ipKey)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := protector.DecryptIP(ciphertext, nonce, ipKey, "test-key")
	if err != nil || decoded != "203.0.113.7" {
		t.Fatalf("decrypt = %q, %v", decoded, err)
	}
	tampered := append([]byte(nil), ciphertext...)
	tampered[0] ^= 1
	if _, err := protector.DecryptIP(tampered, nonce, ipKey, "test-key"); err == nil {
		t.Fatal("tampered ciphertext accepted")
	}
	if _, err := protector.DecryptIP(ciphertext, nonce, protector.LookupKey("ip", "203.0.113.8"), "test-key"); err == nil {
		t.Fatal("ciphertext accepted for another record")
	}
}

func TestIdentityProtectorReadsPreviousEncryptionKey(t *testing.T) {
	oldConfig := testIdentityConfig()
	oldProtector, err := NewIdentityProtector(oldConfig)
	if err != nil {
		t.Fatal(err)
	}
	lookupKey := oldProtector.LookupKey("ip", "8.8.4.4")
	ciphertext, nonce, err := oldProtector.EncryptIP("8.8.4.4", lookupKey)
	if err != nil {
		t.Fatal(err)
	}
	rotated := oldConfig
	rotated.EncryptionKey = base64.StdEncoding.EncodeToString([]byte("ABCDEFabcdefghijklmnopqrstuvwxyz"))
	rotated.EncryptionKeyID = "next-key"
	rotated.PreviousEncryptionKey = oldConfig.EncryptionKey
	rotated.PreviousEncryptionKeyID = oldConfig.EncryptionKeyID
	protector, err := NewIdentityProtector(rotated)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := protector.DecryptIP(ciphertext, nonce, lookupKey, oldConfig.EncryptionKeyID)
	if err != nil || decoded != "8.8.4.4" {
		t.Fatalf("decrypt previous key = %q, %v", decoded, err)
	}
}

func TestNormalizeIdentityIPUnmapsAndRejectsPrivateQuality(t *testing.T) {
	public, err := normalizeIdentityIP("::ffff:203.0.113.9")
	if err != nil || public.Value != "203.0.113.9" || public.Family != 4 {
		t.Fatalf("public = %+v, %v", public, err)
	}
	private, err := normalizeIdentityIP("10.0.0.8")
	if err != nil || private.Public {
		t.Fatalf("private = %+v, %v", private, err)
	}
}

func TestIdentityIPSearchLookupUsesNormalizedPublicAddress(t *testing.T) {
	protector, err := NewIdentityProtector(testIdentityConfig())
	if err != nil {
		t.Fatal(err)
	}
	lookupKey, valid := identityIPSearchLookup(protector, "  ::ffff:8.8.8.8  ")
	if !valid || lookupKey != protector.LookupKey("ip", "8.8.8.8") {
		t.Fatalf("lookup = %q, valid = %v", lookupKey, valid)
	}
	if lookupKey, valid = identityIPSearchLookup(protector, "10.0.0.8"); valid || lookupKey != "" {
		t.Fatalf("private lookup = %q, valid = %v", lookupKey, valid)
	}
	if lookupKey, valid = identityIPSearchLookup(protector, ""); !valid || lookupKey != "" {
		t.Fatalf("empty lookup = %q, valid = %v", lookupKey, valid)
	}
}
