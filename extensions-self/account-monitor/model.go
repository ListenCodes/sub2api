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
	GroupID              *int64
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
	Attempts        []AttemptFact
	Requests        []RequestFact
	GroupDimensions []GroupDimension
	UsageCursor     Cursor
	ErrorCursor     Cursor
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

type CursorQuality struct {
	CursorTime    *time.Time `json:"cursor_time"`
	CursorID      int64      `json:"cursor_id"`
	LastSuccessAt *time.Time `json:"last_success_at"`
	LastError     string     `json:"last_error,omitempty"`
}

type DataQualitySnapshot struct {
	DataAsOf               *time.Time    `json:"data_as_of"`
	CollectionLagSeconds   *float64      `json:"collection_lag_seconds"`
	StaleDataWarning       string        `json:"stale_data_warning,omitempty"`
	UsageCursor            CursorQuality `json:"usage_cursor"`
	ErrorCursor            CursorQuality `json:"error_cursor"`
	RecentSourceError      string        `json:"recent_source_error,omitempty"`
	AvailableFrom          *time.Time    `json:"available_from"`
	AvailableTo            *time.Time    `json:"available_to"`
	MissingGroupRequests   int64         `json:"missing_group_requests"`
	ExactModelRequests     int64         `json:"exact_model_requests"`
	EstimatedModelRequests int64         `json:"estimated_model_requests"`
}
