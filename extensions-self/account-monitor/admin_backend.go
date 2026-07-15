package accountmonitor

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
)

const overviewAttemptsSQL = `
SELECT COUNT(*),COUNT(*) FILTER (WHERE result='succeeded'),COUNT(*) FILTER (WHERE result='failed'),
       COUNT(DISTINCT account_id),COUNT(DISTINCT user_id) FILTER (WHERE user_id IS NOT NULL),
       COALESCE(SUM(input_tokens+output_tokens+cache_creation_tokens+cache_read_tokens),0),
       COALESCE(SUM(user_cost),0),COALESCE(SUM(account_cost),0),
       COALESCE(percentile_disc(0.95) WITHIN GROUP (ORDER BY duration_ms) FILTER (WHERE duration_ms IS NOT NULL),0)
FROM account_monitor_attempt_facts WHERE attempted_at >= $1 AND attempted_at < $2`

const overviewRequestsSQL = `
SELECT COUNT(*),COUNT(*) FILTER (WHERE result='succeeded')
FROM account_monitor_request_facts WHERE occurred_at >= $1 AND occurred_at < $2`

const syncOverviewSQL = `
SELECT COALESCE(MAX(last_success_at),to_timestamp(0)),
       EXTRACT(EPOCH FROM (NOW()-COALESCE(MAX(last_success_at),NOW())))
FROM account_monitor_sync_state`

const accountBaseSQL = `
SELECT CASE WHEN $3='parent' THEN COALESCE(parent_account_id,account_id) ELSE account_id END AS rollup_account_id,
       MAX(parent_account_id),MAX(platform),COUNT(*) AS attempts,
       COUNT(*) FILTER (WHERE result='succeeded') AS successes,
       COUNT(*) FILTER (WHERE result='failed') AS failures,
       COALESCE(SUM(input_tokens+output_tokens+cache_creation_tokens+cache_read_tokens),0) AS tokens,
       COALESCE(SUM(user_cost),0) AS user_cost,COALESCE(SUM(account_cost),0) AS account_cost,
       COALESCE(AVG(duration_ms) FILTER (WHERE duration_ms IS NOT NULL),0) AS avg_duration_ms,
       COALESCE(percentile_disc(0.95) WITHIN GROUP (ORDER BY duration_ms) FILTER (WHERE duration_ms IS NOT NULL),0) AS p95_duration_ms,
       COALESCE(MAX(attempted_at) FILTER (WHERE result='succeeded'),to_timestamp(0)) AS last_success_at,
       COALESCE(MAX(attempted_at) FILTER (WHERE result='failed'),to_timestamp(0)) AS last_failure_at,
       COUNT(DISTINCT actual_model) AS model_count,COUNT(DISTINCT user_id) FILTER (WHERE user_id IS NOT NULL) AS user_count,
       COALESCE(SUM(image_count),0),COALESCE(SUM(video_count),0),COALESCE(SUM(video_duration_seconds),0),
       COUNT(*) OVER() AS total
FROM account_monitor_attempt_facts
WHERE attempted_at >= $1 AND attempted_at < $2
  AND ($6='' OR platform=$6) AND ($7='' OR actual_model ILIKE '%'||$7||'%')
  AND ($8='' OR result=$8) AND ($9='' OR error_category=$9)
  AND ($10=0 OR account_id=$10 OR parent_account_id=$10)
GROUP BY 1`

const modelsSQL = `
SELECT actual_model,model_attribution,COUNT(*),COUNT(*) FILTER (WHERE result='succeeded'),
       COUNT(*) FILTER (WHERE result='failed'),
       COALESCE(SUM(input_tokens+output_tokens+cache_creation_tokens+cache_read_tokens),0),
       COALESCE(SUM(user_cost),0),COALESCE(SUM(account_cost),0),
       COALESCE(AVG(duration_ms) FILTER (WHERE duration_ms IS NOT NULL),0),
       COALESCE(percentile_disc(0.95) WITHIN GROUP (ORDER BY duration_ms) FILTER (WHERE duration_ms IS NOT NULL),0),
       COUNT(*) OVER()
FROM account_monitor_attempt_facts
WHERE attempted_at >= $1 AND attempted_at < $2 AND (account_id=$3 OR parent_account_id=$3)
GROUP BY actual_model,model_attribution ORDER BY COUNT(*) DESC,actual_model LIMIT $4 OFFSET $5`

const usersSQL = `
SELECT COALESCE(user_id,0),COALESCE(api_key_id,0),COUNT(*),
       COUNT(*) FILTER (WHERE result='succeeded'),COUNT(*) FILTER (WHERE result='failed'),
       COALESCE(SUM(input_tokens+output_tokens+cache_creation_tokens+cache_read_tokens),0),
       COALESCE(SUM(user_cost),0),COUNT(*) OVER()
FROM account_monitor_attempt_facts
WHERE attempted_at >= $1 AND attempted_at < $2 AND (account_id=$3 OR parent_account_id=$3)
GROUP BY user_id,api_key_id ORDER BY COUNT(*) DESC,user_id,api_key_id LIMIT $4 OFFSET $5`

const errorsSQL = `
SELECT error_category,COALESCE(upstream_status_code,0),provider_error_code,COUNT(*),
       COUNT(*) FILTER (WHERE recovered),MAX(attempted_at),COUNT(*) OVER()
FROM account_monitor_attempt_facts
WHERE result='failed' AND attempted_at >= $1 AND attempted_at < $2 AND (account_id=$3 OR parent_account_id=$3)
GROUP BY error_category,upstream_status_code,provider_error_code
ORDER BY COUNT(*) DESC,error_category LIMIT $4 OFFSET $5`

const attemptsSQL = `
SELECT event_key,request_key,attempted_at,account_id,platform,actual_model,model_attribution,
       COALESCE(user_id,0),COALESCE(api_key_id,0),request_type,result,recovered,error_category,
       COALESCE(status_code,0),COALESCE(upstream_status_code,0),provider_error_code,
       input_tokens+output_tokens+cache_creation_tokens+cache_read_tokens,user_cost,account_cost,
       COALESCE(duration_ms,0),image_count,image_size,video_count,video_resolution,video_duration_seconds,
       identity_quality,COUNT(*) OVER()
FROM account_monitor_attempt_facts
WHERE attempted_at >= $1 AND attempted_at < $2 AND ($3=0 OR account_id=$3 OR parent_account_id=$3)
ORDER BY attempted_at DESC,id DESC LIMIT $4 OFFSET $5`

const dataQualitySQL = `
SELECT COUNT(*) FILTER (WHERE model_attribution='exact'),COUNT(*) FILTER (WHERE model_attribution='estimated'),
       COUNT(*) FILTER (WHERE identity_quality='fallback'),COUNT(*) FILTER (WHERE recovered)
FROM account_monitor_attempt_facts WHERE attempted_at >= $1 AND attempted_at < $2`

const requestQualitySQL = `
SELECT COUNT(*) FILTER (WHERE result='failed' AND account_id IS NULL),COUNT(*) FILTER (WHERE result='failed')
FROM account_monitor_request_facts WHERE occurred_at >= $1 AND occurred_at < $2`

const selectThresholdSQL = `SELECT config FROM account_monitor_thresholds WHERE scope_type='global' AND scope_id=0`
const selectThresholdOverridesSQL = `SELECT scope_type,scope_id,config FROM account_monitor_thresholds ORDER BY scope_type,scope_id`
const healthMetricsSQL = `
SELECT CASE WHEN $3='parent' THEN COALESCE(parent_account_id,account_id) ELSE account_id END AS rollup_account_id,
       COUNT(*),COUNT(*) FILTER (WHERE result='succeeded'),COUNT(*) FILTER (WHERE result='failed'),
       MAX(attempted_at) FILTER (WHERE result='succeeded')
FROM account_monitor_attempt_facts
WHERE attempted_at >= $1 AND attempted_at < $2
  AND (cardinality($4::bigint[])=0 OR (CASE WHEN $3='parent' THEN COALESCE(parent_account_id,account_id) ELSE account_id END)=ANY($4))
GROUP BY 1 ORDER BY 1`
const upsertThresholdSQL = `
INSERT INTO account_monitor_thresholds(scope_type,scope_id,config,updated_by,updated_at)
VALUES ($1,$2,$3,$4,NOW())
ON CONFLICT (scope_type,scope_id) DO UPDATE SET config=EXCLUDED.config,updated_by=EXCLUDED.updated_by,updated_at=NOW()`

const getRebuildJobSQL = `
SELECT id,from_time,to_time,status,processed_rows,error,requested_by,created_at,started_at,completed_at
FROM account_monitor_rebuild_jobs WHERE id=$1`

type AdminService struct {
	repo         *Repository
	source       *PostgresSource
	queryTimeout time.Duration
	now          func() time.Time
}

func NewAdminService(repo *Repository, source *PostgresSource, queryTimeout time.Duration) *AdminService {
	if queryTimeout <= 0 {
		queryTimeout = 3 * time.Second
	}
	return &AdminService{repo: repo, source: source, queryTimeout: queryTimeout, now: func() time.Time { return time.Now().UTC() }}
}

type OverviewResponse struct {
	Attempts         int64     `json:"attempts"`
	Successes        int64     `json:"successes"`
	Failures         int64     `json:"failures"`
	Requests         int64     `json:"requests"`
	RequestSuccesses int64     `json:"request_successes"`
	ActiveAccounts   int64     `json:"active_accounts"`
	AbnormalAccounts int64     `json:"abnormal_accounts"`
	Users            int64     `json:"users"`
	Tokens           int64     `json:"tokens"`
	UserCost         float64   `json:"user_cost"`
	AccountCost      float64   `json:"account_cost"`
	P95DurationMS    int64     `json:"p95_duration_ms"`
	LastSyncAt       time.Time `json:"last_sync_at"`
	SyncLagSeconds   float64   `json:"sync_lag_seconds"`
}

type AccountSummary struct {
	AccountID            int64     `json:"account_id"`
	ParentAccountID      int64     `json:"parent_account_id,omitempty"`
	AccountName          string    `json:"account_name"`
	Platform             string    `json:"platform"`
	Status               string    `json:"status"`
	Attempts             int64     `json:"attempts"`
	Successes            int64     `json:"successes"`
	Failures             int64     `json:"failures"`
	Tokens               int64     `json:"tokens"`
	UserCost             float64   `json:"user_cost"`
	AccountCost          float64   `json:"account_cost"`
	AverageDurationMS    float64   `json:"average_duration_ms"`
	P95DurationMS        int64     `json:"p95_duration_ms"`
	LastSuccessAt        time.Time `json:"last_success_at"`
	LastFailureAt        time.Time `json:"last_failure_at"`
	ModelCount           int64     `json:"model_count"`
	UserCount            int64     `json:"user_count"`
	ImageCount           int64     `json:"image_count"`
	VideoCount           int64     `json:"video_count"`
	VideoDurationSeconds int64     `json:"video_duration_seconds"`
	Health               Health    `json:"health"`
}

type PageResponse struct {
	Items    any   `json:"items"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
}

type ThresholdResponse struct {
	Scope       ThresholdScope `json:"scope"`
	ScopeID     int64          `json:"scope_id"`
	SuccessRate float64        `json:"success_rate"`
}

type DataQualityResponse struct {
	SourceConnected      bool     `json:"source_connected"`
	ErrorAttributionRate *float64 `json:"error_attribution_rate,omitempty"`
	UnattributedErrors   int64    `json:"unattributed_errors"`
	RecoveredFailures    int64    `json:"recovered_failures"`
	ExactModels          int64    `json:"exact_models"`
	EstimatedModels      int64    `json:"estimated_models"`
	FallbackIdentities   int64    `json:"fallback_identities"`
	DataSource           string   `json:"data_source"`
}

func (s *AdminService) ExecuteAdmin(ctx context.Context, request AdminRequest) (any, error) {
	if s == nil || s.repo == nil || s.repo.db == nil {
		return nil, errors.New("account monitor repository is unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	switch request.Resource {
	case ResourceOverview:
		return s.overview(ctx, request)
	case ResourceAccounts:
		return s.accounts(ctx, request)
	case ResourceAccount:
		return s.account(ctx, request)
	case ResourceModels:
		return s.models(ctx, request)
	case ResourceUsers:
		return s.users(ctx, request)
	case ResourceErrors:
		return s.errors(ctx, request)
	case ResourceAttempts:
		return s.attempts(ctx, request)
	case ResourceDataQuality:
		return s.dataQuality(ctx, request)
	case ResourceThresholds:
		if request.Method == http.MethodPut {
			return s.putThreshold(ctx, request)
		}
		return s.threshold(ctx)
	case ResourceRebuildJobs:
		return s.repo.CreateRebuildJob(ctx, request.From, request.To, request.ActorID)
	case ResourceRebuildJob:
		return s.rebuildJob(ctx, request.JobID)
	default:
		return nil, errors.New("unsupported account monitor resource")
	}
}

func (s *AdminService) overview(ctx context.Context, request AdminRequest) (OverviewResponse, error) {
	var result OverviewResponse
	var p95 float64
	if err := s.repo.db.QueryRowContext(ctx, overviewAttemptsSQL, request.From, request.To).Scan(
		&result.Attempts, &result.Successes, &result.Failures, &result.ActiveAccounts,
		&result.Users, &result.Tokens, &result.UserCost, &result.AccountCost, &p95,
	); err != nil {
		return result, err
	}
	result.P95DurationMS = int64(p95)
	if err := s.repo.db.QueryRowContext(ctx, overviewRequestsSQL, request.From, request.To).Scan(&result.Requests, &result.RequestSuccesses); err != nil {
		return result, err
	}
	if err := s.repo.db.QueryRowContext(ctx, syncOverviewSQL).Scan(&result.LastSyncAt, &result.SyncLagSeconds); err != nil {
		return result, err
	}
	healthByAccount, err := s.evaluateAccountHealth(ctx, "physical", nil)
	if err != nil {
		return result, err
	}
	for _, health := range healthByAccount {
		if health.Level != HealthNormal {
			result.AbnormalAccounts++
		}
	}
	return result, nil
}

func accountSortClause(sortBy, order string) string {
	columns := map[string]string{"attempts": "attempts", "success_rate": "success_rate", "failures": "failures", "tokens": "tokens", "user_cost": "user_cost", "p95_duration_ms": "p95_duration_ms"}
	column, ok := columns[sortBy]
	if !ok {
		return "attempts DESC, rollup_account_id ASC"
	}
	direction := "DESC"
	if strings.EqualFold(order, "asc") {
		direction = "ASC"
	}
	return column + " " + direction + ", rollup_account_id ASC"
}

func (s *AdminService) accounts(ctx context.Context, request AdminRequest) (PageResponse, error) {
	rollup := request.Query["rollup"]
	if rollup != "parent" {
		rollup = "physical"
	}
	accountID, _ := strconv.ParseInt(request.Query["account_id"], 10, 64)
	query := `SELECT stats.*,CASE WHEN attempts=0 THEN 0 ELSE successes::float/attempts END AS success_rate FROM (` + accountBaseSQL + `) stats ORDER BY ` + accountSortClause(request.SortBy, request.SortOrder) + ` LIMIT $4 OFFSET $5`
	rows, err := s.repo.db.QueryContext(ctx, query, request.From, request.To, rollup, request.PageSize, (request.Page-1)*request.PageSize, request.Query["platform"], request.Query["model"], request.Query["result"], request.Query["error_category"], accountID)
	if err != nil {
		return PageResponse{}, err
	}
	defer rows.Close()
	items := make([]AccountSummary, 0)
	var total int64
	for rows.Next() {
		var item AccountSummary
		var successRate float64
		if err := rows.Scan(&item.AccountID, &item.ParentAccountID, &item.Platform, &item.Attempts, &item.Successes, &item.Failures,
			&item.Tokens, &item.UserCost, &item.AccountCost, &item.AverageDurationMS, &item.P95DurationMS,
			&item.LastSuccessAt, &item.LastFailureAt, &item.ModelCount, &item.UserCount, &item.ImageCount,
			&item.VideoCount, &item.VideoDurationSeconds, &total, &successRate); err != nil {
			return PageResponse{}, err
		}
		item.AccountName = fmt.Sprintf("账号 %d", item.AccountID)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return PageResponse{}, err
	}
	healthByAccount, err := s.evaluateAccountHealth(ctx, rollup, items)
	if err != nil {
		return PageResponse{}, err
	}
	for index := range items {
		health, ok := healthByAccount[items[index].AccountID]
		if !ok {
			health = Health{Level: HealthNormal, Reasons: []string{}}
		}
		items[index].Health = health
	}
	if s.source != nil && len(items) > 0 {
		ids := make([]int64, 0, len(items))
		for _, item := range items {
			ids = append(ids, item.AccountID)
		}
		if dimensions, err := s.source.ReadDimensions(ctx, DimensionIDs{AccountIDs: ids}); err == nil {
			for index := range items {
				if dimension, ok := dimensions.Accounts[items[index].AccountID]; ok {
					items[index].AccountName, items[index].Status = dimension.Name, dimension.Status
				}
			}
		}
	}
	return PageResponse{Items: items, Total: total, Page: request.Page, PageSize: request.PageSize}, nil
}

func (s *AdminService) evaluateAccountHealth(ctx context.Context, rollup string, accounts []AccountSummary) (map[int64]Health, error) {
	overrides, err := s.loadThresholdOverrides(ctx)
	if err != nil {
		return nil, err
	}
	accountIDs := make([]int64, 0, len(accounts))
	contexts := make(map[int64]ThresholdContext, len(accounts))
	for _, account := range accounts {
		accountIDs = append(accountIDs, account.AccountID)
		contexts[account.AccountID] = ThresholdContext{ParentAccountID: account.ParentAccountID, AccountID: account.AccountID}
	}
	now := s.now()
	rows, err := s.repo.db.QueryContext(ctx, healthMetricsSQL, now.Add(-time.Hour), now, rollup, pq.Array(accountIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[int64]Health)
	for rows.Next() {
		var accountID, attempts, successes, failures int64
		var lastSuccess sql.NullTime
		if err := rows.Scan(&accountID, &attempts, &successes, &failures, &lastSuccess); err != nil {
			return nil, err
		}
		metrics := HealthMetrics{Attempts1H: attempts, Successes1H: successes, Failures1H: failures}
		if lastSuccess.Valid {
			metrics.LastSuccessAt = lastSuccess.Time
		}
		thresholds := ResolveThresholds(DefaultThresholds(), overrides, contexts[accountID])
		result[accountID] = EvaluateHealth(metrics, thresholds, now)
	}
	return result, rows.Err()
}

func (s *AdminService) loadThresholdOverrides(ctx context.Context) ([]ThresholdOverride, error) {
	rows, err := s.repo.db.QueryContext(ctx, selectThresholdOverridesSQL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	overrides := make([]ThresholdOverride, 0)
	for rows.Next() {
		var override ThresholdOverride
		var raw []byte
		if err := rows.Scan(&override.Scope, &override.ScopeID, &raw); err != nil {
			return nil, err
		}
		var config struct {
			SuccessRate *float64 `json:"success_rate"`
		}
		if err := json.Unmarshal(raw, &config); err != nil {
			return nil, err
		}
		override.SuccessRate = config.SuccessRate
		overrides = append(overrides, override)
	}
	return overrides, rows.Err()
}

func (s *AdminService) account(ctx context.Context, request AdminRequest) (AccountSummary, error) {
	request.Query["account_id"] = strconv.FormatInt(request.AccountID, 10)
	request.Page, request.PageSize = 1, 1
	page, err := s.accounts(ctx, request)
	if err != nil {
		return AccountSummary{}, err
	}
	items := page.Items.([]AccountSummary)
	if len(items) == 0 {
		return AccountSummary{}, sql.ErrNoRows
	}
	return items[0], nil
}

func (s *AdminService) models(ctx context.Context, request AdminRequest) (PageResponse, error) {
	rows, err := s.repo.db.QueryContext(ctx, modelsSQL, request.From, request.To, request.AccountID, request.PageSize, (request.Page-1)*request.PageSize)
	if err != nil {
		return PageResponse{}, err
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	var total int64
	for rows.Next() {
		var model, attribution string
		var attempts, successes, failures, tokens, p95 int64
		var userCost, accountCost, average float64
		if err := rows.Scan(&model, &attribution, &attempts, &successes, &failures, &tokens, &userCost, &accountCost, &average, &p95, &total); err != nil {
			return PageResponse{}, err
		}
		items = append(items, map[string]any{"actual_model": model, "model_attribution": attribution, "attempts": attempts, "successes": successes, "failures": failures, "tokens": tokens, "user_cost": userCost, "account_cost": accountCost, "average_duration_ms": average, "p95_duration_ms": p95})
	}
	return PageResponse{Items: items, Total: total, Page: request.Page, PageSize: request.PageSize}, rows.Err()
}

func (s *AdminService) users(ctx context.Context, request AdminRequest) (PageResponse, error) {
	rows, err := s.repo.db.QueryContext(ctx, usersSQL, request.From, request.To, request.AccountID, request.PageSize, (request.Page-1)*request.PageSize)
	if err != nil {
		return PageResponse{}, err
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	var total int64
	userIDs := []int64{}
	keyIDs := []int64{}
	for rows.Next() {
		var userID, keyID, attempts, successes, failures, tokens int64
		var cost float64
		if err := rows.Scan(&userID, &keyID, &attempts, &successes, &failures, &tokens, &cost, &total); err != nil {
			return PageResponse{}, err
		}
		items = append(items, map[string]any{"user_id": userID, "api_key_id": keyID, "attempts": attempts, "successes": successes, "failures": failures, "tokens": tokens, "user_cost": cost})
		userIDs = append(userIDs, userID)
		keyIDs = append(keyIDs, keyID)
	}
	if s.source != nil {
		if dims, err := s.source.ReadDimensions(ctx, DimensionIDs{UserIDs: userIDs, APIKeyIDs: keyIDs}); err == nil {
			for _, item := range items {
				uid := item["user_id"].(int64)
				kid := item["api_key_id"].(int64)
				if d, ok := dims.Users[uid]; ok {
					item["email"], item["username"] = d.Email, d.Username
				}
				if d, ok := dims.APIKeys[kid]; ok {
					item["api_key_name"], item["masked_prefix"] = d.Name, d.MaskedPrefix
				}
			}
		}
	}
	return PageResponse{Items: items, Total: total, Page: request.Page, PageSize: request.PageSize}, rows.Err()
}

func (s *AdminService) errors(ctx context.Context, request AdminRequest) (PageResponse, error) {
	rows, err := s.repo.db.QueryContext(ctx, errorsSQL, request.From, request.To, request.AccountID, request.PageSize, (request.Page-1)*request.PageSize)
	if err != nil {
		return PageResponse{}, err
	}
	defer rows.Close()
	items := []map[string]any{}
	var total int64
	for rows.Next() {
		var category, code string
		var status int
		var count, recovered int64
		var last time.Time
		if err := rows.Scan(&category, &status, &code, &count, &recovered, &last, &total); err != nil {
			return PageResponse{}, err
		}
		items = append(items, map[string]any{"error_category": category, "upstream_status_code": status, "provider_error_code": code, "failures": count, "recovered_failures": recovered, "last_failure_at": last})
	}
	return PageResponse{Items: items, Total: total, Page: request.Page, PageSize: request.PageSize}, rows.Err()
}

func (s *AdminService) attempts(ctx context.Context, request AdminRequest) (PageResponse, error) {
	accountID := request.AccountID
	if accountID == 0 {
		accountID, _ = strconv.ParseInt(request.Query["account_id"], 10, 64)
	}
	rows, err := s.repo.db.QueryContext(ctx, attemptsSQL, request.From, request.To, accountID, request.PageSize, (request.Page-1)*request.PageSize)
	if err != nil {
		return PageResponse{}, err
	}
	defer rows.Close()
	items := []map[string]any{}
	var total int64
	for rows.Next() {
		var eventKey, requestKey, platform, model, attribution, result, category, providerCode, imageSize, videoResolution, identity string
		var at time.Time
		var accountID, userID, keyID int64
		var requestType, status, upstreamStatus, imageCount, videoCount, videoDuration int
		var recovered bool
		var tokens, duration int64
		var userCost, accountCost float64
		if err := rows.Scan(&eventKey, &requestKey, &at, &accountID, &platform, &model, &attribution, &userID, &keyID, &requestType, &result, &recovered, &category, &status, &upstreamStatus, &providerCode, &tokens, &userCost, &accountCost, &duration, &imageCount, &imageSize, &videoCount, &videoResolution, &videoDuration, &identity, &total); err != nil {
			return PageResponse{}, err
		}
		items = append(items, map[string]any{"event_key": eventKey, "request_key": requestKey, "attempted_at": at, "account_id": accountID, "platform": platform, "actual_model": model, "model_attribution": attribution, "user_id": userID, "api_key_id": keyID, "request_type": requestType, "result": result, "recovered": recovered, "error_category": category, "status_code": status, "upstream_status_code": upstreamStatus, "provider_error_code": providerCode, "tokens": tokens, "user_cost": userCost, "account_cost": accountCost, "duration_ms": duration, "image_count": imageCount, "image_size": imageSize, "video_count": videoCount, "video_resolution": videoResolution, "video_duration_seconds": videoDuration, "identity_quality": identity})
	}
	return PageResponse{Items: items, Total: total, Page: request.Page, PageSize: request.PageSize}, rows.Err()
}

func (s *AdminService) dataQuality(ctx context.Context, request AdminRequest) (DataQualityResponse, error) {
	result := DataQualityResponse{DataSource: "90 天明细"}
	if s.source != nil {
		result.SourceConnected = s.source.Ping(ctx) == nil
	}
	if err := s.repo.db.QueryRowContext(ctx, dataQualitySQL, request.From, request.To).Scan(&result.ExactModels, &result.EstimatedModels, &result.FallbackIdentities, &result.RecoveredFailures); err != nil {
		return result, err
	}
	var failed int64
	if err := s.repo.db.QueryRowContext(ctx, requestQualitySQL, request.From, request.To).Scan(&result.UnattributedErrors, &failed); err != nil {
		return result, err
	}
	if failed > 0 {
		rate := float64(failed-result.UnattributedErrors) / float64(failed)
		result.ErrorAttributionRate = &rate
	}
	return result, nil
}

func (s *AdminService) threshold(ctx context.Context) (ThresholdResponse, error) {
	result := ThresholdResponse{Scope: ScopeGlobal, SuccessRate: DefaultThresholds().SuccessRate}
	var raw []byte
	err := s.repo.db.QueryRowContext(ctx, selectThresholdSQL).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	_ = json.Unmarshal(raw, &result)
	return result, nil
}

func (s *AdminService) putThreshold(ctx context.Context, request AdminRequest) (ThresholdResponse, error) {
	var input ThresholdResponse
	if err := json.Unmarshal(request.Body, &input); err != nil {
		return input, err
	}
	if input.Scope == "" {
		input.Scope = ScopeGlobal
	}
	if input.SuccessRate <= 0 || input.SuccessRate > 1 {
		return input, errors.New("success_rate must be between 0 and 1")
	}
	switch input.Scope {
	case ScopeGlobal, ScopePlatform, ScopeParent, ScopeAccount:
	default:
		return input, errors.New("invalid threshold scope")
	}
	raw, _ := json.Marshal(input)
	_, err := s.repo.db.ExecContext(ctx, upsertThresholdSQL, input.Scope, input.ScopeID, raw, request.ActorID)
	return input, err
}

func (s *AdminService) rebuildJob(ctx context.Context, id int64) (RebuildJob, error) {
	var job RebuildJob
	var started, completed sql.NullTime
	err := s.repo.db.QueryRowContext(ctx, getRebuildJobSQL, id).Scan(&job.ID, &job.From, &job.To, &job.Status, &job.ProcessedRows, &job.Error, &job.RequestedBy, &job.CreatedAt, &started, &completed)
	if started.Valid {
		job.StartedAt = &started.Time
	}
	if completed.Valid {
		job.CompletedAt = &completed.Time
	}
	return job, err
}
