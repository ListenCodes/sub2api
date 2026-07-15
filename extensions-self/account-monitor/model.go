package accountmonitor

import "time"

type Result string

type ErrorCategory string

const (
	ResultSucceeded Result = "succeeded"
	ResultFailed    Result = "failed"
)

type AttributionQuality string

const (
	AttributionExact     AttributionQuality = "exact"
	AttributionEstimated AttributionQuality = "estimated"
)

type IdentityQuality string

const (
	IdentityExact    IdentityQuality = "exact"
	IdentityFallback IdentityQuality = "fallback"
)

type AttemptFact struct {
	EventKey             string
	RequestKey           string
	AttemptedAt          time.Time
	AccountID            int64
	ParentAccountID      int64
	Platform             string
	ActualModel          string
	ModelAttribution     AttributionQuality
	UserID               int64
	APIKeyID             int64
	RequestType          int
	Result               Result
	Recovered            bool
	ErrorCategory        ErrorCategory
	StatusCode           int
	UpstreamStatusCode   int
	ProviderErrorCode    string
	InputTokens          int64
	OutputTokens         int64
	CacheCreationTokens  int64
	CacheReadTokens      int64
	UserCost             float64
	AccountCost          float64
	DurationMS           int64
	ImageCount           int
	ImageSize            string
	VideoCount           int
	VideoResolution      string
	VideoDurationSeconds int
	IdentityQuality      IdentityQuality
	SourceKind           string
	SourceID             int64
}

type RequestFact struct {
	RequestKey           string
	OccurredAt           time.Time
	UserID               int64
	APIKeyID             int64
	AccountID            int64
	Platform             string
	ActualModel          string
	ModelAttribution     AttributionQuality
	RequestType          int
	Result               Result
	ErrorCategory        ErrorCategory
	StatusCode           int
	InputTokens          int64
	OutputTokens         int64
	CacheCreationTokens  int64
	CacheReadTokens      int64
	UserCost             float64
	AccountCost          float64
	DurationMS           int64
	ImageCount           int
	VideoCount           int
	VideoResolution      string
	VideoDurationSeconds int
	IdentityQuality      IdentityQuality
	SourceKind           string
	SourceID             int64
}

type Batch struct {
	Attempts    []AttemptFact
	Requests    []RequestFact
	UsageCursor Cursor
	ErrorCursor Cursor
}

type RebuildStatus string

const (
	RebuildPending   RebuildStatus = "pending"
	RebuildRunning   RebuildStatus = "running"
	RebuildCompleted RebuildStatus = "completed"
	RebuildFailed    RebuildStatus = "failed"
)

type RebuildJob struct {
	ID            int64         `json:"id"`
	From          time.Time     `json:"from"`
	To            time.Time     `json:"to"`
	Status        RebuildStatus `json:"status"`
	ProcessedRows int64         `json:"processed_rows"`
	Error         string        `json:"error,omitempty"`
	RequestedBy   int64         `json:"requested_by"`
	CreatedAt     time.Time     `json:"created_at"`
	StartedAt     *time.Time    `json:"started_at,omitempty"`
	CompletedAt   *time.Time    `json:"completed_at,omitempty"`
}
