package main

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
)

var identityTokenPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)

const maximumAPISuccessDeliveryAge = 47 * time.Hour

type IdentityService struct {
	cfg       IdentityConfig
	repo      *SQLIdentityRepository
	protector *IdentityProtector
}

func NewIdentityService(cfg IdentityConfig, repo *SQLIdentityRepository) (*IdentityService, error) {
	protector, err := NewIdentityProtector(cfg)
	if err != nil {
		return nil, err
	}
	return &IdentityService{cfg: cfg, repo: repo, protector: protector}, nil
}

func (s *IdentityService) Ingest(ctx context.Context, input IdentityEventReport) (bool, error) {
	if s == nil || !s.cfg.Enabled {
		return false, nil
	}
	if s.repo == nil || s.protector == nil {
		return false, errors.New("identity service unavailable")
	}
	fact, err := s.prepare(input)
	if err != nil {
		return false, err
	}
	stored, duplicate, err := s.repo.Persist(ctx, fact)
	if err != nil || duplicate {
		return duplicate, err
	}
	if err := s.repo.EvaluateAndStoreSignals(ctx, stored, s.cfg); err != nil {
		// Signal generation is advisory and must never invalidate the stored fact.
		return false, nil
	}
	return false, nil
}

func (s *IdentityService) prepare(input IdentityEventReport) (IdentityFact, error) {
	input.EventKey = strings.TrimSpace(input.EventKey)
	input.EventType = strings.TrimSpace(input.EventType)
	input.EventClass = strings.TrimSpace(input.EventClass)
	input.Outcome = strings.TrimSpace(input.Outcome)
	if input.EventKey == "" || len(input.EventKey) > 240 || !identityTokenPattern.MatchString(input.EventKey) {
		return IdentityFact{}, identityErr("event_key")
	}
	if input.EventType == "" || len(input.EventType) > 80 || !identityTokenPattern.MatchString(input.EventType) {
		return IdentityFact{}, identityErr("event_type")
	}
	switch input.EventClass {
	case "registration", "login", "oauth", identityEventAPI, "security":
	default:
		return IdentityFact{}, identityErr("event_class")
	}
	switch input.Outcome {
	case "attempt", "success", "failure", "observed":
	default:
		return IdentityFact{}, identityErr("outcome")
	}
	occurredAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(input.OccurredAt))
	if err != nil || occurredAt.After(time.Now().UTC().Add(2*time.Minute)) {
		return IdentityFact{}, identityErr("occurred_at")
	}
	if input.EventClass == identityEventAPI && input.Outcome == "success" && occurredAt.Before(time.Now().UTC().Add(-maximumAPISuccessDeliveryAge)) {
		return IdentityFact{}, identityErr("occurred_at")
	}
	fact := IdentityFact{EventKey: input.EventKey, EventType: input.EventType, EventClass: input.EventClass, Outcome: input.Outcome, OccurredAt: occurredAt.UTC(), UserID: input.UserID, ProxyChainValid: input.ProxyChainValid}
	email := strings.ToLower(strings.TrimSpace(input.Email))
	if len(email) > 320 {
		return IdentityFact{}, identityErr("email")
	}
	if email != "" {
		fact.EmailLookupKey = s.protector.LookupKey("email", email)
	}

	if s.cfg.IPCollectionEnabled && strings.TrimSpace(input.ClientIP) != "" {
		normalized, normalizeErr := normalizeIdentityIP(input.ClientIP)
		validSource := input.IPSource == "remote_addr" || input.IPSource == "trusted_xff" || input.IPSource == "trusted_real_ip" || input.IPSource == "cf_connecting_ip"
		if normalizeErr == nil && normalized.Public && input.ProxyChainValid && validSource {
			lookupKey := s.protector.LookupKey("ip", normalized.Value)
			ciphertext, nonce, encryptErr := s.protector.EncryptIP(normalized.Value, lookupKey)
			if encryptErr != nil {
				return IdentityFact{}, encryptErr
			}
			geoVerified := input.GeoVerified && (input.GeoSource == "cloudflare_verified" || input.GeoSource == "maxmind_local")
			countryCode := strings.ToUpper(strings.TrimSpace(input.CountryCode))
			if len(countryCode) != 2 {
				countryCode = ""
			}
			fact.Network = &IdentityNetworkFact{LookupKey: lookupKey, PrefixLookupKey: s.protector.LookupKey("ip-prefix", normalized.PrefixValue), Ciphertext: ciphertext, Nonce: nonce, KeyID: s.protector.keyID, Family: normalized.Family, Source: validateIdentityText(input.IPSource, 40), Public: true, CountryCode: countryCode, Region: validateIdentityText(input.Region, 80), City: validateIdentityText(input.City, 120), ASN: input.ASN, GeoSource: validateIdentityText(input.GeoSource, 40), GeoVerified: geoVerified}
			fact.IPQualityValid = true
		}
	}

	if !s.cfg.DeviceCollectionEnabled {
		return fact, nil
	}
	if input.APIKeyID > 0 {
		fact.APIClient = &IdentityDeviceFact{Kind: "api_client", LookupKey: s.protector.LookupKey("api-client", formatUserID(input.APIKeyID))}
		fact.DeviceQualityValid = true
	} else {
		browserID := strings.TrimSpace(input.BrowserInstanceID)
		if browserID != "" && len(browserID) <= 128 && identityTokenPattern.MatchString(browserID) {
			fact.Browser = &IdentityDeviceFact{Kind: "browser_instance", LookupKey: s.protector.LookupKey("browser-instance", browserID), CookieStatus: validateIdentityText(input.BrowserCookieStatus, 24)}
			fact.DeviceQualityValid = input.BrowserCookieStatus == "valid" || input.BrowserCookieStatus == "issued" || input.BrowserCookieStatus == "rotated"
		}
		profileValue := strings.Join([]string{strings.ToLower(input.BrowserFamily), strings.ToLower(input.OSFamily), strings.ToLower(input.DeviceClass), strings.ToLower(input.LanguageFamily)}, "|")
		if strings.Trim(profileValue, "|") != "" {
			fact.Profile = &IdentityDeviceFact{Kind: "browser_profile", LookupKey: s.protector.LookupKey("browser-profile", profileValue), BrowserFamily: validateIdentityText(input.BrowserFamily, 40), OSFamily: validateIdentityText(input.OSFamily, 40), DeviceClass: validateIdentityText(input.DeviceClass, 24), LanguageFamily: validateIdentityText(input.LanguageFamily, 24)}
		}
	}
	return fact, nil
}

func (s *IdentityService) Summary(ctx context.Context, userID int64) (IdentitySummary, error) {
	return s.repo.Summary(ctx, userID, s.cfg)
}

func (s *IdentityService) Health(ctx context.Context) (IdentityHealth, error) {
	if s == nil || s.repo == nil {
		return IdentityHealth{Enabled: false, Mode: "shadow", Schema: "v2", Domains: map[string]string{"ip": "disabled", "device": "disabled", "composite": "disabled"}, Quality24H: map[string]any{}}, nil
	}
	return s.repo.Health(ctx, s.cfg)
}
