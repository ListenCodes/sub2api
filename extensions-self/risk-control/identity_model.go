package main

import "time"

const (
	identityVersionV2 = "v2"
	identityEventAPI  = "api_success"
)

type IdentityEventReport struct {
	EventKey            string `json:"event_key"`
	EventType           string `json:"event_type"`
	EventClass          string `json:"event_class"`
	Outcome             string `json:"outcome"`
	OccurredAt          string `json:"occurred_at"`
	UserID              int64  `json:"user_id,omitempty"`
	Email               string `json:"email,omitempty"`
	ClientIP            string `json:"client_ip,omitempty"`
	IPSource            string `json:"ip_source,omitempty"`
	ProxyChainValid     bool   `json:"proxy_chain_valid"`
	CountryCode         string `json:"country_code,omitempty"`
	Region              string `json:"region,omitempty"`
	City                string `json:"city,omitempty"`
	ASN                 int64  `json:"asn,omitempty"`
	GeoSource           string `json:"geo_source,omitempty"`
	GeoVerified         bool   `json:"geo_verified"`
	BrowserInstanceID   string `json:"browser_instance_id,omitempty"`
	BrowserCookieStatus string `json:"browser_cookie_status,omitempty"`
	BrowserFamily       string `json:"browser_family,omitempty"`
	OSFamily            string `json:"os_family,omitempty"`
	DeviceClass         string `json:"device_class,omitempty"`
	LanguageFamily      string `json:"language_family,omitempty"`
	APIKeyID            int64  `json:"api_key_id,omitempty"`
}

type IdentityNetworkFact struct {
	LookupKey       string
	PrefixLookupKey string
	Ciphertext      []byte
	Nonce           []byte
	KeyID           string
	Family          int
	Source          string
	Public          bool
	CountryCode     string
	Region          string
	City            string
	ASN             int64
	GeoSource       string
	GeoVerified     bool
}

type IdentityDeviceFact struct {
	Kind           string
	LookupKey      string
	BrowserFamily  string
	OSFamily       string
	DeviceClass    string
	LanguageFamily string
	CookieStatus   string
}

type IdentityFact struct {
	EventKey           string
	EventType          string
	EventClass         string
	Outcome            string
	OccurredAt         time.Time
	UserID             int64
	EmailLookupKey     string
	Network            *IdentityNetworkFact
	Browser            *IdentityDeviceFact
	Profile            *IdentityDeviceFact
	APIClient          *IdentityDeviceFact
	IPQualityValid     bool
	DeviceQualityValid bool
	ProxyChainValid    bool
}

type PersistedIdentityEvent struct {
	ID                 int64
	UserID             int64
	EmailLookupKey     string
	NetworkID          int64
	BrowserID          int64
	ProfileID          int64
	APIClientID        int64
	EventType          string
	EventClass         string
	Outcome            string
	OccurredAt         time.Time
	IPQualityValid     bool
	DeviceQualityValid bool
}

type IdentityDomainSummary struct {
	Domain                 string                  `json:"domain"`
	State                  string                  `json:"state"`
	Score                  int                     `json:"score"`
	SignalCount            int                     `json:"signal_count"`
	AssociatedAccountCount int                     `json:"associated_account_count"`
	Signals                []IdentitySignalSummary `json:"signals"`
}

type IdentitySignalSummary struct {
	RuleCode      string `json:"rule_code"`
	Score         int    `json:"score"`
	EvidenceCount int    `json:"evidence_count"`
	OccurredAt    string `json:"occurred_at"`
}

type IdentitySummary struct {
	UserID          int64                   `json:"user_id"`
	IdentityVersion string                  `json:"identity_version"`
	Mode            string                  `json:"mode"`
	OverallScore    int                     `json:"overall_score"`
	LegacyNotice    string                  `json:"legacy_notice"`
	Domains         []IdentityDomainSummary `json:"domains"`
}

type NetworkIdentityRow struct {
	ID                     int64  `json:"id"`
	LookupKey              string `json:"-"`
	Ciphertext             []byte `json:"-"`
	Nonce                  []byte `json:"-"`
	KeyID                  string `json:"-"`
	IP                     string `json:"ip"`
	IPFamily               int    `json:"ip_family"`
	IPSource               string `json:"ip_source"`
	Public                 bool   `json:"is_public"`
	CountryCode            string `json:"country_code"`
	Region                 string `json:"region"`
	City                   string `json:"city"`
	ASN                    int64  `json:"asn"`
	GeoSource              string `json:"geo_source"`
	GeoVerified            bool   `json:"geo_verified"`
	FirstSeenAt            string `json:"first_seen_at"`
	LastSeenAt             string `json:"last_seen_at"`
	RegistrationSuccesses  int64  `json:"registration_success_count"`
	LoginSuccesses         int64  `json:"login_success_count"`
	APISuccesses           int64  `json:"api_success_count"`
	AssociatedAccountCount int    `json:"associated_account_count"`
}

type DeviceIdentityRow struct {
	ID                     int64  `json:"id"`
	Kind                   string `json:"identity_kind"`
	LookupKey              string `json:"-"`
	DisplayCode            string `json:"display_code"`
	Confidence             string `json:"confidence"`
	BrowserFamily          string `json:"browser_family"`
	OSFamily               string `json:"os_family"`
	DeviceClass            string `json:"device_class"`
	LanguageFamily         string `json:"language_family"`
	CookieStatus           string `json:"cookie_status"`
	FirstSeenAt            string `json:"first_seen_at"`
	LastSeenAt             string `json:"last_seen_at"`
	RegistrationSuccesses  int64  `json:"registration_success_count"`
	LoginSuccesses         int64  `json:"login_success_count"`
	APISuccesses           int64  `json:"api_success_count"`
	NetworkCount           int    `json:"network_count"`
	AssociatedAccountCount int    `json:"associated_account_count"`
}

type AssociatedUserRow struct {
	UserID                   int64  `json:"user_id"`
	Relation                 string `json:"relation"`
	SharedNetworkCount       int    `json:"shared_network_count"`
	SharedDeviceCount        int    `json:"shared_device_count"`
	CooccurringEvidenceCount int    `json:"cooccurring_evidence_count"`
	EvidenceStrength         string `json:"evidence_strength"`
	EvidenceWindowSeconds    int    `json:"evidence_window_seconds"`
	FirstSeenAt              string `json:"first_seen_at"`
	LastSeenAt               string `json:"last_seen_at"`
}

type IdentityListSummary struct {
	UserID                 int64  `json:"user_id"`
	LatestIP               string `json:"latest_ip"`
	CountryCode            string `json:"country_code"`
	Region                 string `json:"region"`
	BrowserInstanceCount   int    `json:"browser_instance_count"`
	APIClientCount         int    `json:"api_client_count"`
	AssociatedAccountCount int    `json:"associated_account_count"`
	ActiveRuleCount        int    `json:"active_rule_count"`
	QualityState           string `json:"quality_state"`
	HasIdentity            bool   `json:"-"`
	LookupKey              string `json:"-"`
	Ciphertext             []byte `json:"-"`
	Nonce                  []byte `json:"-"`
	KeyID                  string `json:"-"`
}

type IdentityHealth struct {
	Enabled      bool              `json:"enabled"`
	AdminEnabled bool              `json:"admin_enabled"`
	Mode         string            `json:"mode"`
	ShadowUntil  string            `json:"shadow_until,omitempty"`
	Schema       string            `json:"schema"`
	KeyID        string            `json:"key_id,omitempty"`
	GeoSource    string            `json:"geo_source"`
	Domains      map[string]string `json:"domains"`
	Quality24H   map[string]any    `json:"quality_24h"`
}

type IdentityRule struct {
	Code              string `json:"code"`
	Domain            string `json:"domain"`
	ConfiguredEnabled bool   `json:"configured_enabled"`
	Enabled           bool   `json:"enabled"`
	State             string `json:"state"`
	WindowSeconds     int    `json:"window_seconds"`
	Threshold         int    `json:"threshold"`
	Score             int    `json:"score"`
	Mode              string `json:"mode"`
	Revision          int    `json:"revision"`
	UpdatedAt         string `json:"updated_at"`
}

type LegacyV1CleanupResult struct {
	Applied         bool  `json:"applied"`
	EventsDeleted   int64 `json:"events_deleted"`
	SubjectsDeleted int64 `json:"subjects_deleted"`
	RulesDeleted    int64 `json:"rules_deleted"`
}

type RebuildResult struct {
	ID                 int64            `json:"id"`
	DryRun             bool             `json:"dry_run"`
	Status             string           `json:"status"`
	LegacyAPISubjects  int64            `json:"legacy_api_subjects"`
	CurrentSignalUsers int64            `json:"current_signal_users"`
	V2SignalUsers      int64            `json:"v2_signal_users"`
	CurrentSignals     int64            `json:"current_signals"`
	V2Signals          int64            `json:"v2_signals"`
	ChangedSubjects    int64            `json:"changed_subjects"`
	RuleHits           map[string]int64 `json:"rule_hits"`
	SampleUserIDs      []int64          `json:"sample_user_ids"`
	StartedAt          string           `json:"started_at"`
	CompletedAt        string           `json:"completed_at,omitempty"`
}
