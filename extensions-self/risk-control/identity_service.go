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
	if err != nil {
		return duplicate, err
	}
	if stored.ID > 0 {
		if err := s.repo.ProcessSignalJob(ctx, input.EventKey, s.cfg); err != nil {
			return duplicate, err
		}
	}
	return duplicate, nil
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
			countryCode, region, city, asn, geoSource := "", "", "", int64(0), ""
			if geoVerified {
				countryCode = strings.ToUpper(strings.TrimSpace(input.CountryCode))
				if !validIdentityCountryCode(countryCode) {
					countryCode = ""
				}
				region = validateIdentityText(input.Region, 80)
				city = validateIdentityText(input.City, 120)
				if input.ASN > 0 && input.ASN <= 4294967295 {
					asn = input.ASN
				}
				geoSource = input.GeoSource
				geoVerified = countryCode != "" || region != "" || city != "" || asn > 0
			}
			if !geoVerified {
				countryCode, region, city, asn, geoSource = "", "", "", 0, ""
			}
			fact.Network = &IdentityNetworkFact{LookupKey: lookupKey, PrefixLookupKey: s.protector.LookupKey("ip-prefix", normalized.PrefixValue), Ciphertext: ciphertext, Nonce: nonce, KeyID: s.protector.keyID, Family: normalized.Family, Source: validateIdentityText(input.IPSource, 40), Public: true, CountryCode: countryCode, Region: region, City: city, ASN: asn, GeoSource: geoSource, GeoVerified: geoVerified}
			fact.IPQualityValid = true
		}
	}

	if !s.cfg.DeviceCollectionEnabled {
		fact.EvidenceSnapshot = identityFactSnapshot(fact)
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
	fact.EvidenceSnapshot = identityFactSnapshot(fact)
	return fact, nil
}

func identityFactSnapshot(fact IdentityFact) map[string]any {
	snapshot := map[string]any{
		"event_key": fact.EventKey, "event_type": fact.EventType, "event_class": fact.EventClass,
		"outcome": fact.Outcome, "user_id": fact.UserID, "occurred_at": fact.OccurredAt.UTC().Format(time.RFC3339Nano),
		"proxy_chain_valid": fact.ProxyChainValid, "ip_quality_valid": fact.IPQualityValid,
		"device_quality_valid": fact.DeviceQualityValid,
	}
	if fact.Network != nil {
		snapshot["network"] = map[string]any{"lookup_key": fact.Network.LookupKey, "prefix_lookup_key": fact.Network.PrefixLookupKey, "ip_family": fact.Network.Family, "ip_source": fact.Network.Source, "country_code": fact.Network.CountryCode, "region": fact.Network.Region, "city": fact.Network.City, "asn": fact.Network.ASN, "geo_source": fact.Network.GeoSource, "geo_verified": fact.Network.GeoVerified}
	}
	if fact.Browser != nil {
		snapshot["browser_instance"] = map[string]any{"lookup_key": fact.Browser.LookupKey, "cookie_status": fact.Browser.CookieStatus}
	}
	if fact.Profile != nil {
		snapshot["browser_profile"] = map[string]any{"lookup_key": fact.Profile.LookupKey, "browser_family": fact.Profile.BrowserFamily, "os_family": fact.Profile.OSFamily, "device_class": fact.Profile.DeviceClass, "language_family": fact.Profile.LanguageFamily}
	}
	if fact.APIClient != nil {
		snapshot["api_client"] = map[string]any{"lookup_key": fact.APIClient.LookupKey}
	}
	return snapshot
}

func validIdentityCountryCode(value string) bool {
	if len(value) != 2 || value == "XX" {
		return false
	}
	for _, character := range value {
		if character < 'A' || character > 'Z' {
			return false
		}
	}
	return true
}

func (s *IdentityService) Summary(ctx context.Context, userID int64) (IdentitySummary, error) {
	return s.repo.Summary(ctx, userID, s.cfg)
}

func (s *IdentityService) Health(ctx context.Context) (IdentityHealth, error) {
	if s == nil || s.repo == nil {
		return IdentityHealth{Enabled: false, AdminEnabled: false, Mode: "shadow", Schema: "v2", Domains: map[string]string{"ip": "disabled", "device": "disabled", "composite": "disabled"}, QualityDomains: map[string]string{"ip": "disabled", "device": "disabled", "composite": "disabled"}, Quality24H: map[string]any{}}, nil
	}
	return s.repo.Health(ctx, s.cfg)
}

func (s *IdentityService) Rules(ctx context.Context) ([]IdentityRule, error) {
	if s == nil || s.repo == nil {
		return nil, errors.New("identity service unavailable")
	}
	rules, err := s.repo.ListIdentityRules(ctx)
	if err != nil {
		return nil, err
	}
	states, _, err := s.repo.qualityStates(ctx, s.cfg)
	if err != nil {
		return nil, err
	}
	for index := range rules {
		state := identityRuleEffectiveState(s.cfg, rules[index].Domain, states)
		rules[index].State = state
		rules[index].Enabled = rules[index].ConfiguredEnabled && state == "healthy"
	}
	return rules, nil
}

func identityRuleEffectiveState(cfg IdentityConfig, domain string, states map[string]string) string {
	if !identityRuleDomainEnabled(cfg, domain) {
		return "disabled"
	}
	if domain == "account" {
		return "healthy"
	}
	state := states[domain]
	if state == "" {
		return "disabled"
	}
	return state
}

func identityRuleDomainEnabled(cfg IdentityConfig, domain string) bool {
	if !cfg.RulesEnabled {
		return false
	}
	switch domain {
	case "account":
		return true
	case "ip":
		return cfg.IPCollectionEnabled && cfg.IPDomainEnabled
	case "device":
		return cfg.DeviceCollectionEnabled && cfg.DeviceDomainEnabled
	case "composite":
		return cfg.IPCollectionEnabled && cfg.DeviceCollectionEnabled && cfg.IPDomainEnabled && cfg.DeviceDomainEnabled && cfg.CompositeDomainEnabled
	default:
		return false
	}
}
