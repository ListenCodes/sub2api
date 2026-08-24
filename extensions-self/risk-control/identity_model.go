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
	EvidenceSnapshot   map[string]any
}

type CompositeRegistrationEvaluation struct {
	RuleCode      string
	WindowSeconds int
	Threshold     int
	Score         int
	Revision      int
	AccountCount  int
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
	HistoricalMaxScore     int                     `json:"historical_max_score"`
	HistoricalSignalCount  int                     `json:"historical_signal_count"`
}

type IdentitySignalSummary struct {
	RuleCode         string         `json:"rule_code"`
	RuleRevision     int            `json:"rule_revision"`
	SignalFamily     string         `json:"signal_family"`
	Status           string         `json:"status"`
	DecisionID       string         `json:"decision_id"`
	Score            int            `json:"score"`
	EvidenceCount    int            `json:"evidence_count"`
	EvidenceSnapshot map[string]any `json:"evidence_snapshot"`
	OccurredAt       string         `json:"occurred_at"`
	ActiveUntil      string         `json:"active_until,omitempty"`
}

type IdentitySummary struct {
	UserID                int64                   `json:"user_id"`
	IdentityVersion       string                  `json:"identity_version"`
	Mode                  string                  `json:"mode"`
	OverallScore          int                     `json:"overall_score"`
	HistoricalMaxScore    int                     `json:"historical_max_score"`
	HistoricalSignalCount int                     `json:"historical_signal_count"`
	LegacyNotice          string                  `json:"legacy_notice"`
	Domains               []IdentityDomainSummary `json:"domains"`
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
	Availability           string `json:"availability"`
	UnavailableReason      string `json:"unavailable_reason,omitempty"`
	UnavailableImpact      string `json:"unavailable_impact,omitempty"`
	DataSource             string `json:"data_source"`
	NetworkLabel           string `json:"network_label,omitempty"`
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
	UserID                     int64    `json:"user_id"`
	Relation                   string   `json:"relation"`
	SharedNetworkCount         int      `json:"shared_network_count"`
	SharedBrowserInstanceCount int      `json:"shared_browser_instance_count"`
	SharedAPIClientCount       int      `json:"shared_api_client_count"`
	SharedDeviceCount          int      `json:"shared_device_count"`
	CooccurringEvidenceCount   int      `json:"cooccurring_evidence_count"`
	EvidenceStrength           string   `json:"evidence_strength"`
	EvidenceWindowSeconds      int      `json:"evidence_window_seconds"`
	Concurrent                 bool     `json:"concurrent"`
	OverlapStart               string   `json:"overlap_start,omitempty"`
	OverlapEnd                 string   `json:"overlap_end,omitempty"`
	FirstSeenAt                string   `json:"first_seen_at"`
	LastSeenAt                 string   `json:"last_seen_at"`
	SourceEventIDs             []int64  `json:"source_event_ids"`
	Limitations                []string `json:"limitations"`
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
	Enabled              bool              `json:"enabled"`
	AdminEnabled         bool              `json:"admin_enabled"`
	Mode                 string            `json:"mode"`
	ShadowUntil          string            `json:"shadow_until,omitempty"`
	Schema               string            `json:"schema"`
	KeyID                string            `json:"key_id,omitempty"`
	GeoSource            string            `json:"geo_source"`
	Domains              map[string]string `json:"domains"`
	QualityDomains       map[string]string `json:"quality_domains"`
	Quality24H           map[string]any    `json:"quality_24h"`
	Delivery             map[string]any    `json:"delivery"`
	Processing           map[string]any    `json:"processing"`
	Features             map[string]bool   `json:"features"`
	ConfiguredRuleCount  int64             `json:"configured_rule_count"`
	ProspectiveRuleCount int64             `json:"prospective_rule_count"`
	EffectiveRuleCount   int64             `json:"effective_rule_count"`
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
	SignalFamily      string `json:"signal_family"`
	SubjectKind       string `json:"subject_kind"`
	ActiveFrom        string `json:"active_from"`
	ActiveUntil       string `json:"active_until,omitempty"`
	UpdatedAt         string `json:"updated_at"`
}

type IdentityDeliveryReport struct {
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

type RiskReviewCase struct {
	ID                 int64  `json:"id"`
	UserID             int64  `json:"user_id"`
	DecisionID         string `json:"decision_id"`
	SignalFamily       string `json:"signal_family"`
	Status             string `json:"status"`
	Resolution         string `json:"resolution"`
	CurrentScore       int    `json:"current_score"`
	HistoricalMaxScore int    `json:"historical_max_score"`
	PrimarySignal      string `json:"primary_signal"`
	EvidenceStrength   string `json:"evidence_strength"`
	AssigneeID         int64  `json:"assignee_id"`
	OpenedAt           string `json:"opened_at"`
	LastHitAt          string `json:"last_hit_at"`
	ResolvedAt         string `json:"resolved_at,omitempty"`
}

type RiskRuleEffect struct {
	RuleCode             string  `json:"rule_code"`
	Revision             int     `json:"revision"`
	HitEvents            int64   `json:"hit_events"`
	UniqueSubjects       int64   `json:"unique_subjects"`
	SampleUserIDs        []int64 `json:"sample_user_ids"`
	ConfirmedRate        float64 `json:"confirmed_rate"`
	LegitimateSharedRate float64 `json:"legitimate_shared_rate"`
	MissingSignalRate    float64 `json:"missing_signal_rate"`
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
	EvidenceHighWater  int64            `json:"evidence_high_water"`
	RuleWatermark      map[string]int   `json:"rule_watermark"`
	ApprovedDryRunID   int64            `json:"approved_dry_run_id,omitempty"`
	StartedAt          string           `json:"started_at"`
	CompletedAt        string           `json:"completed_at,omitempty"`
}
