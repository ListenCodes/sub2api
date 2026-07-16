package accountmonitor

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/lib/pq"
)

type Cursor struct {
	Time time.Time `json:"time"`
	ID   int64     `json:"id"`
}

type UsageSourceRow struct {
	ID                    int64
	CreatedAt             time.Time
	UserID                int64
	APIKeyID              int64
	AccountID             int64
	GroupID               *int64
	ParentAccountID       int64
	RequestID             string
	Platform              string
	Model                 string
	RequestedModel        string
	UpstreamModel         string
	InputTokens           int64
	OutputTokens          int64
	CacheCreationTokens   int64
	CacheReadTokens       int64
	TotalCost             float64
	ActualCost            float64
	AccountRateMultiplier float64
	AccountMultiplierSet  bool
	DurationMS            int64
	RequestType           int
	Stream                bool
	ImageCount            int
	ImageSize             string
	ImageInputSize        string
	ImageOutputSize       string
	ImageSizeBreakdown    json.RawMessage
	VideoCount            int
	VideoResolution       string
	VideoDurationSeconds  int
}

type ErrorSourceRow struct {
	ID                   int64
	CreatedAt            time.Time
	RequestID            string
	ClientRequestID      string
	UserID               int64
	APIKeyID             int64
	AccountID            int64
	GroupID              *int64
	Platform             string
	Model                string
	RequestedModel       string
	UpstreamModel        string
	RequestType          int
	Stream               bool
	ErrorPhase           string
	ErrorType            string
	ErrorSource          string
	ErrorOwner           string
	StatusCode           int
	UpstreamStatusCode   int
	ProviderErrorCode    string
	ProviderErrorType    string
	NetworkErrorType     string
	DurationMS           int64
	ErrorMessage         string
	UpstreamErrorMessage string
	UpstreamErrors       json.RawMessage
}

type DimensionIDs struct {
	AccountIDs []int64
	UserIDs    []int64
	APIKeyIDs  []int64
	GroupIDs   []int64
}

type AccountDimension struct {
	ID              int64
	ParentAccountID int64
	Name            string
	Platform        string
	Status          string
	Schedulable     bool
	DeletedAt       *time.Time
}

type UserDimension struct {
	ID        int64
	Email     string
	Username  string
	Status    string
	DeletedAt *time.Time
}

type APIKeyDimension struct {
	ID           int64
	UserID       int64
	Name         string
	MaskedPrefix string
	Status       string
	DeletedAt    *time.Time
}

type GroupDimension struct {
	ID             int64
	Name           string
	Platform       string
	Status         string
	RateMultiplier float64
	DeletedAt      *time.Time
	SyncedAt       time.Time
}

type AccountGroupDimension struct {
	AccountID int64
	Group     GroupDimension
}

type Dimensions struct {
	Accounts map[int64]AccountDimension
	Users    map[int64]UserDimension
	APIKeys  map[int64]APIKeyDimension
	Groups   map[int64]GroupDimension
}

type PostgresSource struct {
	db           *sql.DB
	queryTimeout time.Duration
	batchSize    int
}

func NewPostgresSource(db *sql.DB, queryTimeout time.Duration, batchSize int) *PostgresSource {
	if queryTimeout <= 0 {
		queryTimeout = 3 * time.Second
	}
	if batchSize <= 0 {
		batchSize = 1000
	}
	return &PostgresSource{db: db, queryTimeout: queryTimeout, batchSize: batchSize}
}

func (s *PostgresSource) Ping(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("account monitor source database is nil")
	}
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	return s.db.PingContext(ctx)
}

func (s *PostgresSource) ReadUsage(ctx context.Context, after Cursor, from time.Time, limit int) ([]UsageSourceRow, error) {
	return s.readUsage(ctx, usageSourceQuery, from, after.Time, after.ID, s.pageSize(limit))
}

func (s *PostgresSource) ReadUsageRange(ctx context.Context, after Cursor, from, to time.Time, limit int) ([]UsageSourceRow, error) {
	return s.readUsage(ctx, usageRangeSourceQuery, from, to, after.Time, after.ID, s.pageSize(limit))
}

func (s *PostgresSource) readUsage(ctx context.Context, query string, args ...any) ([]UsageSourceRow, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("account monitor source database is nil")
	}
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]UsageSourceRow, 0)
	for rows.Next() {
		var item UsageSourceRow
		var groupID, parentID, duration, videoDuration sql.NullInt64
		var requestedModel, upstreamModel, imageSize, imageInputSize, imageOutputSize, videoResolution sql.NullString
		var accountMultiplier sql.NullFloat64
		var imageSizeBreakdown []byte
		if err := rows.Scan(
			&item.ID, &item.CreatedAt, &item.UserID, &item.APIKeyID, &item.AccountID, &groupID, &parentID,
			&item.RequestID, &item.Platform, &item.Model, &requestedModel, &upstreamModel,
			&item.InputTokens, &item.OutputTokens, &item.CacheCreationTokens, &item.CacheReadTokens,
			&item.TotalCost, &item.ActualCost, &accountMultiplier, &duration,
			&item.RequestType, &item.Stream, &item.ImageCount, &imageSize, &imageInputSize,
			&imageOutputSize, &imageSizeBreakdown, &item.VideoCount, &videoResolution, &videoDuration,
		); err != nil {
			return nil, err
		}
		item.ParentAccountID = parentID.Int64
		item.GroupID = nullableInt64Ptr(groupID)
		item.RequestedModel = requestedModel.String
		item.UpstreamModel = upstreamModel.String
		item.AccountRateMultiplier = accountMultiplier.Float64
		item.AccountMultiplierSet = accountMultiplier.Valid
		item.DurationMS = duration.Int64
		item.ImageSize = imageSize.String
		item.ImageInputSize = imageInputSize.String
		item.ImageOutputSize = imageOutputSize.String
		item.ImageSizeBreakdown = imageSizeBreakdown
		item.VideoResolution = videoResolution.String
		item.VideoDurationSeconds = int(videoDuration.Int64)
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *PostgresSource) ReadErrors(ctx context.Context, after Cursor, from time.Time, limit int) ([]ErrorSourceRow, error) {
	return s.readErrors(ctx, errorSourceQuery, from, after.Time, after.ID, s.pageSize(limit))
}

func (s *PostgresSource) ReadErrorsRange(ctx context.Context, after Cursor, from, to time.Time, limit int) ([]ErrorSourceRow, error) {
	return s.readErrors(ctx, errorRangeSourceQuery, from, to, after.Time, after.ID, s.pageSize(limit))
}

func (s *PostgresSource) readErrors(ctx context.Context, query string, args ...any) ([]ErrorSourceRow, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("account monitor source database is nil")
	}
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]ErrorSourceRow, 0)
	for rows.Next() {
		var item ErrorSourceRow
		var requestID, clientRequestID, platform, model, requestedModel, upstreamModel sql.NullString
		var errorSource, errorOwner, providerCode, providerType, networkType sql.NullString
		var upstreamMessage sql.NullString
		var userID, apiKeyID, accountID, groupID, requestType, statusCode, upstreamStatus, duration sql.NullInt64
		if err := rows.Scan(
			&item.ID, &item.CreatedAt, &requestID, &clientRequestID, &userID, &apiKeyID,
			&accountID, &groupID, &platform, &model, &requestedModel, &upstreamModel,
			&requestType, &item.Stream, &item.ErrorPhase, &item.ErrorType, &errorSource,
			&errorOwner, &statusCode, &upstreamStatus, &providerCode, &providerType,
			&networkType, &duration, &item.ErrorMessage, &upstreamMessage, &item.UpstreamErrors,
		); err != nil {
			return nil, err
		}
		item.RequestID = requestID.String
		item.ClientRequestID = clientRequestID.String
		item.UserID = userID.Int64
		item.APIKeyID = apiKeyID.Int64
		item.AccountID = accountID.Int64
		item.GroupID = nullableInt64Ptr(groupID)
		item.Platform = platform.String
		item.Model = model.String
		item.RequestedModel = requestedModel.String
		item.UpstreamModel = upstreamModel.String
		item.RequestType = int(requestType.Int64)
		item.ErrorSource = errorSource.String
		item.ErrorOwner = errorOwner.String
		item.StatusCode = int(statusCode.Int64)
		item.UpstreamStatusCode = int(upstreamStatus.Int64)
		item.ProviderErrorCode = providerCode.String
		item.ProviderErrorType = providerType.String
		item.NetworkErrorType = networkType.String
		item.DurationMS = duration.Int64
		item.UpstreamErrorMessage = upstreamMessage.String
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *PostgresSource) ReadDimensions(ctx context.Context, ids DimensionIDs) (Dimensions, error) {
	result := Dimensions{
		Accounts: make(map[int64]AccountDimension),
		Users:    make(map[int64]UserDimension),
		APIKeys:  make(map[int64]APIKeyDimension),
		Groups:   make(map[int64]GroupDimension),
	}
	if s == nil || s.db == nil {
		return result, errors.New("account monitor source database is nil")
	}
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	if err := s.readAccountDimensions(ctx, ids.AccountIDs, result.Accounts); err != nil {
		return result, err
	}
	if err := s.readUserDimensions(ctx, ids.UserIDs, result.Users); err != nil {
		return result, err
	}
	if err := s.readAPIKeyDimensions(ctx, ids.APIKeyIDs, result.APIKeys); err != nil {
		return result, err
	}
	if err := s.readGroupDimensions(ctx, ids.GroupIDs, result.Groups); err != nil {
		return result, err
	}
	return result, nil
}

func (s *PostgresSource) AccountIDsByStatus(ctx context.Context, status string) ([]int64, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("account monitor source database is nil")
	}
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, accountIDsByStatusQuery, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *PostgresSource) ReadAccountDimensions(ctx context.Context) ([]AccountDimension, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("account monitor source database is nil")
	}
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, allAccountDimensionsQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]AccountDimension, 0)
	for rows.Next() {
		var item AccountDimension
		var parent sql.NullInt64
		var deleted sql.NullTime
		if err := rows.Scan(&item.ID, &parent, &item.Name, &item.Platform, &item.Status, &item.Schedulable, &deleted); err != nil {
			return nil, err
		}
		item.ParentAccountID = parent.Int64
		if deleted.Valid {
			item.DeletedAt = &deleted.Time
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *PostgresSource) ReadAccountGroupDimensions(ctx context.Context) ([]AccountGroupDimension, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("account monitor source database is nil")
	}
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, accountGroupDimensionsQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]AccountGroupDimension, 0)
	for rows.Next() {
		var item AccountGroupDimension
		var deleted sql.NullTime
		if err := rows.Scan(
			&item.AccountID,
			&item.Group.ID,
			&item.Group.Name,
			&item.Group.Platform,
			&item.Group.Status,
			&item.Group.RateMultiplier,
			&deleted,
		); err != nil {
			return nil, err
		}
		if deleted.Valid {
			item.Group.DeletedAt = &deleted.Time
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *PostgresSource) ReadGroupDimensions(ctx context.Context) ([]GroupDimension, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("account monitor source database is nil")
	}
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, allGroupDimensionsQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]GroupDimension, 0)
	for rows.Next() {
		var item GroupDimension
		var deleted sql.NullTime
		if err := rows.Scan(&item.ID, &item.Name, &item.Platform, &item.Status, &deleted); err != nil {
			return nil, err
		}
		if deleted.Valid {
			item.DeletedAt = &deleted.Time
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *PostgresSource) pageSize(limit int) int {
	if limit <= 0 || limit > s.batchSize {
		return s.batchSize
	}
	return limit
}

func (s *PostgresSource) readAccountDimensions(ctx context.Context, ids []int64, target map[int64]AccountDimension) error {
	if len(ids) == 0 {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, accountDimensionQuery, pq.Array(ids))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var item AccountDimension
		var parent sql.NullInt64
		var deleted sql.NullTime
		if err := rows.Scan(&item.ID, &parent, &item.Name, &item.Platform, &item.Status, &item.Schedulable, &deleted); err != nil {
			return err
		}
		item.ParentAccountID = parent.Int64
		if deleted.Valid {
			item.DeletedAt = &deleted.Time
		}
		target[item.ID] = item
	}
	return rows.Err()
}

func (s *PostgresSource) readUserDimensions(ctx context.Context, ids []int64, target map[int64]UserDimension) error {
	if len(ids) == 0 {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, userDimensionQuery, pq.Array(ids))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var item UserDimension
		var deleted sql.NullTime
		if err := rows.Scan(&item.ID, &item.Email, &item.Username, &item.Status, &deleted); err != nil {
			return err
		}
		if deleted.Valid {
			item.DeletedAt = &deleted.Time
		}
		target[item.ID] = item
	}
	return rows.Err()
}

func (s *PostgresSource) readAPIKeyDimensions(ctx context.Context, ids []int64, target map[int64]APIKeyDimension) error {
	if len(ids) == 0 {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, apiKeyDimensionQuery, pq.Array(ids))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var item APIKeyDimension
		var deleted sql.NullTime
		if err := rows.Scan(&item.ID, &item.UserID, &item.Name, &item.MaskedPrefix, &item.Status, &deleted); err != nil {
			return err
		}
		if deleted.Valid {
			item.DeletedAt = &deleted.Time
		}
		target[item.ID] = item
	}
	return rows.Err()
}

func (s *PostgresSource) readGroupDimensions(ctx context.Context, ids []int64, target map[int64]GroupDimension) error {
	if len(ids) == 0 {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, groupDimensionQuery, pq.Array(ids))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var item GroupDimension
		var deleted sql.NullTime
		if err := rows.Scan(&item.ID, &item.Name, &item.Platform, &item.Status, &deleted); err != nil {
			return err
		}
		if deleted.Valid {
			item.DeletedAt = &deleted.Time
		}
		target[item.ID] = item
	}
	return rows.Err()
}

func nullableInt64Ptr(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}

const usageSourceQuery = `
SELECT id, created_at, user_id, api_key_id, account_id, group_id, parent_account_id,
       request_id, platform, model, requested_model, upstream_model,
       input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens,
       total_cost, actual_cost, account_rate_multiplier, duration_ms,
       request_type, stream, image_count, image_size, image_input_size,
       image_output_size, image_size_breakdown, video_count,
       video_resolution, video_duration_seconds
FROM extensions_self_ro.usage_source
WHERE created_at >= $1
  AND (created_at, id) > ($2, $3)
ORDER BY created_at, id
LIMIT $4`

const usageRangeSourceQuery = `
SELECT id, created_at, user_id, api_key_id, account_id, group_id, parent_account_id,
       request_id, platform, model, requested_model, upstream_model,
       input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens,
       total_cost, actual_cost, account_rate_multiplier, duration_ms,
       request_type, stream, image_count, image_size, image_input_size,
       image_output_size, image_size_breakdown, video_count,
       video_resolution, video_duration_seconds
FROM extensions_self_ro.usage_source
WHERE created_at >= $1 AND created_at < $2
  AND (created_at, id) > ($3, $4)
ORDER BY created_at, id
LIMIT $5`

const errorSourceQuery = `
SELECT id, created_at, request_id, client_request_id, user_id, api_key_id,
       account_id, group_id, platform, model, requested_model, upstream_model,
       request_type, stream, error_phase, error_type, error_source,
       error_owner, status_code, upstream_status_code, provider_error_code,
       provider_error_type, network_error_type, duration_ms,
       error_message, upstream_error_message, upstream_errors
FROM extensions_self_ro.error_source
WHERE created_at >= $1
  AND (created_at, id) > ($2, $3)
ORDER BY created_at, id
LIMIT $4`

const errorRangeSourceQuery = `
SELECT id, created_at, request_id, client_request_id, user_id, api_key_id,
       account_id, group_id, platform, model, requested_model, upstream_model,
       request_type, stream, error_phase, error_type, error_source,
       error_owner, status_code, upstream_status_code, provider_error_code,
       provider_error_type, network_error_type, duration_ms,
       error_message, upstream_error_message, upstream_errors
FROM extensions_self_ro.error_source
WHERE created_at >= $1 AND created_at < $2
  AND (created_at, id) > ($3, $4)
ORDER BY created_at, id
LIMIT $5`

const accountDimensionQuery = `
SELECT id, parent_account_id, name, platform, status, schedulable, deleted_at
FROM extensions_self_ro.account_dimension
WHERE id = ANY($1)`

const allAccountDimensionsQuery = `
SELECT id, parent_account_id, name, platform, status, schedulable, deleted_at
FROM extensions_self_ro.account_dimension
ORDER BY id`

const accountGroupDimensionsQuery = `
SELECT account_id, group_id, group_name, group_platform, group_status, group_rate_multiplier, group_deleted_at
FROM extensions_self_ro.account_group_dimension
ORDER BY account_id, LOWER(group_platform), LOWER(group_name), group_id`

const accountIDsByStatusQuery = `
SELECT id FROM extensions_self_ro.account_dimension
WHERE status=$1 ORDER BY id`

const userDimensionQuery = `
SELECT id, email, username, status, deleted_at
FROM extensions_self_ro.user_dimension
WHERE id = ANY($1)`

const apiKeyDimensionQuery = `
SELECT id, user_id, name, masked_prefix, status, deleted_at
FROM extensions_self_ro.api_key_dimension
WHERE id = ANY($1)`

const groupDimensionQuery = `
SELECT id, name, platform, status, deleted_at
FROM extensions_self_ro.group_dimension
WHERE id = ANY($1)`

const allGroupDimensionsQuery = `
SELECT id, name, platform, status, deleted_at
FROM extensions_self_ro.group_dimension
ORDER BY id`
