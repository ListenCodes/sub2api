package main

type EventReport struct {
	EventKey         string         `json:"event_key"`
	EventType        string         `json:"event_type"`
	UserID           int64          `json:"user_id,omitempty"`
	SubjectID        string         `json:"subject_id,omitempty"`
	UsernameSnapshot string         `json:"username,omitempty"`
	AccountStatus    string         `json:"account_status,omitempty"`
	EmailHash        string         `json:"email_hash,omitempty"`
	IPHash           string         `json:"ip_hash,omitempty"`
	DeviceHash       string         `json:"device_hash,omitempty"`
	RiskType         string         `json:"risk_type,omitempty"`
	ErrorCode        string         `json:"error_code,omitempty"`
	Reason           string         `json:"reason,omitempty"`
	Endpoint         string         `json:"endpoint,omitempty"`
	Model            string         `json:"model,omitempty"`
	HTTPStatus       int            `json:"http_status,omitempty"`
	OccurredAt       string         `json:"occurred_at,omitempty"`
	Evidence         map[string]any `json:"evidence,omitempty"`

	// These fields are accepted only by tests and are deliberately excluded from JSON.
	Password    string `json:"-"`
	RequestBody string `json:"-"`
	RawDeviceID string `json:"-"`
}

type AuditReport struct {
	AuditKey   string         `json:"audit_key,omitempty"`
	ActorID    int64          `json:"actor_id"`
	Action     string         `json:"action"`
	TargetType string         `json:"target_type"`
	TargetID   string         `json:"target_id"`
	Result     string         `json:"result"`
	Reason     string         `json:"reason,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type Decision struct {
	Action    string   `json:"decision"`
	Score     int      `json:"score"`
	RiskLevel string   `json:"risk_level"`
	Reason    string   `json:"reason"`
	EventID   int64    `json:"event_id,omitempty"`
	RuleCodes []string `json:"rule_codes,omitempty"`
	Mode      string   `json:"mode,omitempty"`
}

func validRiskAction(action string) bool {
	switch action {
	case "allow", "observe", "review", "ban", "reject_candidate":
		return true
	default:
		return false
	}
}
