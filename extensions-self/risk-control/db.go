package main

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
)

//go:embed schema.sql
var schemaSQL string

type SQLRepository struct{ db *sql.DB }

func NewSQLRepository(db *sql.DB) *SQLRepository { return &SQLRepository{db: db} }

const riskSubjectProjectionCTE = `WITH legacy_api_subjects AS MATERIALIZED (
SELECT user_id
FROM risk_subjects
WHERE risk_type='api_request' OR reason ILIKE '%API 请求观察%'
), non_api_counts AS MATERIALIZED (
SELECT e.user_id,
  COUNT(*)::int AS event_count,
  COUNT(DISTINCT NULLIF(e.ip_hash,''))::int AS ip_count,
  COUNT(DISTINCT NULLIF(e.device_hash,''))::int AS device_count
FROM risk_events e
JOIN legacy_api_subjects legacy ON legacy.user_id=e.user_id
WHERE e.event_type<>'api_request'
GROUP BY e.user_id
), non_api_best AS MATERIALIZED (
SELECT DISTINCT ON (e.user_id)
  e.user_id,e.risk_type,e.risk_level,e.score,e.reason,e.decision,e.occurred_at
FROM risk_events e
JOIN legacy_api_subjects legacy ON legacy.user_id=e.user_id
WHERE e.event_type<>'api_request'
ORDER BY e.user_id,e.score DESC,
  CASE e.risk_level WHEN 'critical' THEN 4 WHEN 'high' THEN 3 WHEN 'medium' THEN 2 WHEN 'low' THEN 1 ELSE 0 END DESC,
  CASE e.decision WHEN 'ban' THEN 4 WHEN 'auto_ban' THEN 4 WHEN 'review' THEN 3 WHEN 'observe' THEN 2 ELSE 0 END DESC,
  e.occurred_at DESC,e.id DESC
), projected_risk_subjects AS (
SELECT s.id,s.user_id,s.username,s.email_hash,s.account_status,
  CASE WHEN legacy.user_id IS NOT NULL THEN COALESCE(best.risk_type,'') ELSE s.risk_type END AS risk_type,
  CASE WHEN legacy.user_id IS NOT NULL THEN COALESCE(best.risk_level,'none') ELSE s.risk_level END AS risk_level,
  CASE WHEN legacy.user_id IS NOT NULL THEN COALESCE(best.score,0) ELSE s.score END AS score,
  CASE WHEN legacy.user_id IS NOT NULL THEN COALESCE(best.reason,'') ELSE s.reason END AS reason,
  CASE WHEN legacy.user_id IS NOT NULL THEN COALESCE(counts.event_count,0) ELSE s.event_count END AS event_count,
  CASE WHEN legacy.user_id IS NOT NULL THEN COALESCE(counts.ip_count,0) ELSE s.ip_count END AS ip_count,
  CASE WHEN legacy.user_id IS NOT NULL THEN COALESCE(counts.device_count,0) ELSE s.device_count END AS device_count,
  CASE WHEN legacy.user_id IS NOT NULL THEN COALESCE(best.decision,'') ELSE s.last_action END AS last_action,
  CASE WHEN legacy.user_id IS NOT NULL THEN COALESCE(best.decision='review',FALSE) ELSE s.pending END AS pending,
  CASE WHEN legacy.user_id IS NOT NULL THEN best.occurred_at ELSE s.last_event_at END AS last_event_at
FROM risk_subjects s
LEFT JOIN legacy_api_subjects legacy ON legacy.user_id=s.user_id
LEFT JOIN non_api_counts counts ON counts.user_id=s.user_id
LEFT JOIN non_api_best best ON best.user_id=s.user_id
) `

func ApplySchema(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return errors.New("database is nil")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, schemaSQL); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO risk_schema_migrations(version) VALUES (1) ON CONFLICT (version) DO NOTHING`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO risk_schema_migrations(version) VALUES (2) ON CONFLICT (version) DO NOTHING`); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *SQLRepository) InsertEvent(ctx context.Context, event EventRecord) (EventRecord, bool, error) {
	evidence, _ := json.Marshal(event.Evidence)
	rules, _ := json.Marshal(event.RuleCodes)
	var id int64
	err := r.db.QueryRowContext(ctx, `
INSERT INTO risk_events (event_key,event_type,user_id,subject_id,username_snapshot,account_status_snapshot,email_hash,ip_hash,device_hash,risk_type,error_code,reason,endpoint,model,http_status,evidence,decision,score,risk_level,rule_codes,occurred_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21) ON CONFLICT (event_key) DO NOTHING RETURNING id`,
		event.EventKey, event.EventType, event.UserID, event.SubjectID, event.UsernameSnapshot, event.AccountStatusSnapshot, event.EmailHash, event.IPHash, event.DeviceHash, event.RiskType, event.ErrorCode, event.Reason, event.Endpoint, event.Model, event.HTTPStatus, evidence, event.Decision, event.Score, event.RiskLevel, rules, event.OccurredAt).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		existing, found, findErr := r.GetEventByKey(ctx, event.EventKey)
		return existing, found, findErr
	}
	if err != nil {
		return EventRecord{}, false, err
	}
	event.ID = id
	return event, false, nil
}

func (r *SQLRepository) GetEventByKey(ctx context.Context, key string) (EventRecord, bool, error) {
	row := r.db.QueryRowContext(ctx, eventSelect+` WHERE event_key=$1`, key)
	event, err := scanEvent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return EventRecord{}, false, nil
	}
	return event, err == nil, err
}

func (r *SQLRepository) CountRecent(ctx context.Context, userID int64, subjectID, ipHash, deviceHash, eventType, countStrategy string, since time.Time) (int, error) {
	var count int
	query, args := countRecentQuery(userID, subjectID, ipHash, deviceHash, eventType, countStrategy, since)
	err := r.db.QueryRowContext(ctx, query, args...).Scan(&count)
	return count, err
}

func countRecentQuery(userID int64, subjectID, ipHash, deviceHash, eventType, countStrategy string, since time.Time) (string, []any) {
	switch normalizeCountStrategy(countStrategy) {
	case countStrategyEmailSubjectEvents:
		return `SELECT COUNT(*) FROM risk_events WHERE $1<>'' AND subject_id=$1 AND event_type=$2 AND occurred_at >= $3`, []any{subjectID, eventType, since}
	case countStrategyIPDistinctSuccessUsers:
		return `SELECT COUNT(DISTINCT user_id) FROM risk_events WHERE $1<>'' AND ip_hash=$1 AND user_id>0 AND user_id<>$2 AND event_type='registration_success' AND occurred_at >= $3`, []any{ipHash, userID, since}
	case countStrategyBrowserDistinctSuccessUsers:
		return `SELECT COUNT(DISTINCT user_id) FROM risk_events WHERE $1<>'' AND device_hash=$1 AND user_id>0 AND user_id<>$2 AND event_type='registration_success' AND occurred_at >= $3`, []any{deviceHash, userID, since}
	case countStrategyAPIClientDistinctUsers:
		return `SELECT COUNT(DISTINCT user_id) FROM risk_events WHERE $1<>'' AND device_hash=$1 AND user_id>0 AND user_id<>$2 AND event_type=$3 AND occurred_at >= $4`, []any{deviceHash, userID, eventType, since}
	case countStrategyIPBrowserCooccurrence:
		return `SELECT COUNT(DISTINCT user_id) FROM risk_events WHERE $1<>'' AND $2<>'' AND ip_hash=$1 AND device_hash=$2 AND user_id>0 AND user_id<>$3 AND event_type='registration_success' AND occurred_at >= $4`, []any{ipHash, deviceHash, userID, since}
	default:
		return `SELECT COUNT(*) FROM risk_events WHERE $1>0 AND user_id=$1 AND event_type=$2 AND occurred_at >= $3`, []any{userID, eventType, since}
	}
}

func (r *SQLRepository) ListRules(ctx context.Context) ([]Rule, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,code,name,description,event_types,count_strategy,enabled,window_seconds,threshold,score,risk_level,action,revision FROM risk_rules ORDER BY code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Rule, 0)
	for rows.Next() {
		var rule Rule
		var eventTypes []byte
		if err := rows.Scan(&rule.ID, &rule.Code, &rule.Name, &rule.Description, &eventTypes, &rule.CountStrategy, &rule.Enabled, &rule.WindowSeconds, &rule.Threshold, &rule.Score, &rule.RiskLevel, &rule.Action, &rule.Revision); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(eventTypes, &rule.EventTypes); err != nil {
			return nil, err
		}
		result = append(result, rule)
	}
	return result, rows.Err()
}

func (r *SQLRepository) CreateRule(ctx context.Context, input Rule) (Rule, error) {
	eventTypes, err := json.Marshal(input.EventTypes)
	if err != nil {
		return Rule{}, err
	}
	var rule Rule
	var raw []byte
	input.CountStrategy = normalizeCountStrategy(input.CountStrategy)
	err = r.db.QueryRowContext(ctx, `INSERT INTO risk_rules (code,name,description,event_types,count_strategy,enabled,window_seconds,threshold,score,risk_level,action,revision) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING id,code,name,description,event_types,count_strategy,enabled,window_seconds,threshold,score,risk_level,action,revision`, input.Code, input.Name, input.Description, string(eventTypes), input.CountStrategy, input.Enabled, input.WindowSeconds, input.Threshold, input.Score, input.RiskLevel, input.Action, 1).Scan(&rule.ID, &rule.Code, &rule.Name, &rule.Description, &raw, &rule.CountStrategy, &rule.Enabled, &rule.WindowSeconds, &rule.Threshold, &rule.Score, &rule.RiskLevel, &rule.Action, &rule.Revision)
	if err != nil {
		if isRuleCodeConflict(err) {
			return Rule{}, ErrRuleCodeConflict
		}
		return Rule{}, err
	}
	if err := json.Unmarshal(raw, &rule.EventTypes); err != nil {
		return Rule{}, err
	}
	return rule, nil
}

func isRuleCodeConflict(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505" && (pqErr.Constraint == "" || strings.Contains(pqErr.Constraint, "risk_rules"))
}

func (r *SQLRepository) UpdateRule(ctx context.Context, code string, expectedRevision int, update Rule) (Rule, error) {
	eventTypes, _ := json.Marshal(update.EventTypes)
	var rule Rule
	var raw []byte
	update.CountStrategy = normalizeCountStrategy(update.CountStrategy)
	err := r.db.QueryRowContext(ctx, `UPDATE risk_rules SET name=$2,description=$3,event_types=$4,count_strategy=$5,enabled=$6,window_seconds=$7,threshold=$8,score=$9,risk_level=$10,action=$11,revision=revision+1,updated_at=NOW() WHERE code=$1 AND revision=$12 RETURNING id,code,name,description,event_types,count_strategy,enabled,window_seconds,threshold,score,risk_level,action,revision`, code, update.Name, update.Description, eventTypes, update.CountStrategy, update.Enabled, update.WindowSeconds, update.Threshold, update.Score, update.RiskLevel, update.Action, expectedRevision).Scan(&rule.ID, &rule.Code, &rule.Name, &rule.Description, &raw, &rule.CountStrategy, &rule.Enabled, &rule.WindowSeconds, &rule.Threshold, &rule.Score, &rule.RiskLevel, &rule.Action, &rule.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return Rule{}, ErrRuleRevisionConflict
	}
	if err != nil {
		return Rule{}, err
	}
	if err := json.Unmarshal(raw, &rule.EventTypes); err != nil {
		return Rule{}, err
	}
	return rule, nil
}

func (r *SQLRepository) UpsertSubject(ctx context.Context, event EventRecord) error {
	if event.UserID <= 0 {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current RiskSubject
	err = tx.QueryRowContext(ctx, `SELECT user_id,risk_type,risk_level,score,reason,last_action,pending FROM risk_subjects WHERE user_id=$1 FOR UPDATE`, event.UserID).Scan(&current.UserID, &current.RiskType, &current.RiskLevel, &current.Score, &current.Reason, &current.LastAction, &current.Pending)
	if errors.Is(err, sql.ErrNoRows) {
		current = RiskSubject{UserID: event.UserID}
	} else if err != nil {
		return err
	}
	replaceSignal := shouldReplaceRiskSignal(current, event)
	if !replaceSignal {
		event.RiskType, event.RiskLevel, event.Score, event.Reason = current.RiskType, current.RiskLevel, current.Score, current.Reason
	}
	lastAction := event.Decision
	if !replaceSignal {
		lastAction = current.LastAction
	}
	pending := current.Pending || event.Decision == "review"
	_, err = tx.ExecContext(ctx, `
INSERT INTO risk_subjects (user_id,username,email_hash,account_status,risk_type,risk_level,score,reason,event_count,ip_count,device_count,last_action,pending,last_event_at,updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,(SELECT COUNT(*) FROM risk_events WHERE user_id=$1),(SELECT COUNT(DISTINCT ip_hash) FROM risk_events WHERE user_id=$1 AND ip_hash<>''),(SELECT COUNT(DISTINCT device_hash) FROM risk_events WHERE user_id=$1 AND device_hash<>''),$9,$10,$11,NOW())
ON CONFLICT (user_id) DO UPDATE SET username=EXCLUDED.username,email_hash=EXCLUDED.email_hash,account_status=EXCLUDED.account_status,risk_type=EXCLUDED.risk_type,risk_level=EXCLUDED.risk_level,score=EXCLUDED.score,reason=EXCLUDED.reason,event_count=EXCLUDED.event_count,ip_count=EXCLUDED.ip_count,device_count=EXCLUDED.device_count,last_action=EXCLUDED.last_action,pending=EXCLUDED.pending,last_event_at=EXCLUDED.last_event_at,updated_at=NOW()`,
		event.UserID, event.UsernameSnapshot, event.EmailHash, event.AccountStatusSnapshot, event.RiskType, event.RiskLevel, event.Score, event.Reason, lastAction, pending, event.OccurredAt)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *SQLRepository) ListSubjects(ctx context.Context, limit, offset int, riskType, riskLevel string, userIDs []int64) ([]RiskSubject, int, error) {
	where := []string{"1=1"}
	args := []any{}
	index := 1
	if riskType != "" {
		where = append(where, fmt.Sprintf("risk_type=$%d", index))
		args = append(args, riskType)
		index++
	}
	if riskLevel != "" {
		where = append(where, fmt.Sprintf("risk_level=$%d", index))
		args = append(args, riskLevel)
		index++
	}
	if len(userIDs) > 0 {
		where = append(where, fmt.Sprintf("user_id = ANY($%d)", index))
		args = append(args, pq.Array(userIDs))
		index++
	}
	whereClause := strings.Join(where, " AND ")
	var total int
	countSource := `SELECT COUNT(*) FROM risk_subjects WHERE `
	if riskType != "" || riskLevel != "" {
		countSource = riskSubjectProjectionCTE + `SELECT COUNT(*) FROM projected_risk_subjects WHERE `
	}
	if err := r.db.QueryRowContext(ctx, countSource+whereClause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.QueryContext(ctx, riskSubjectProjectionCTE+`SELECT id,user_id,username,email_hash,account_status,risk_type,risk_level,score,reason,event_count,ip_count,device_count,last_action,pending,COALESCE(to_char(last_event_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.MS"Z"'),'') FROM projected_risk_subjects WHERE `+whereClause+fmt.Sprintf(" ORDER BY score DESC,last_event_at DESC NULLS LAST LIMIT $%d OFFSET $%d", index, index+1), append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]RiskSubject, 0)
	for rows.Next() {
		var item RiskSubject
		if err := rows.Scan(&item.ID, &item.UserID, &item.Username, &item.EmailHash, &item.AccountStatus, &item.RiskType, &item.RiskLevel, &item.Score, &item.Reason, &item.EventCount, &item.IPCount, &item.DeviceCount, &item.LastAction, &item.Pending, &item.LastEventAt); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (r *SQLRepository) GetSubject(ctx context.Context, userID int64) (RiskSubject, bool, error) {
	var item RiskSubject
	err := r.db.QueryRowContext(ctx, riskSubjectProjectionCTE+`SELECT id,user_id,username,email_hash,account_status,risk_type,risk_level,score,reason,event_count,ip_count,device_count,last_action,pending,COALESCE(to_char(last_event_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.MS"Z"'),'') FROM projected_risk_subjects WHERE user_id=$1`, userID).Scan(&item.ID, &item.UserID, &item.Username, &item.EmailHash, &item.AccountStatus, &item.RiskType, &item.RiskLevel, &item.Score, &item.Reason, &item.EventCount, &item.IPCount, &item.DeviceCount, &item.LastAction, &item.Pending, &item.LastEventAt)
	if errors.Is(err, sql.ErrNoRows) {
		return RiskSubject{}, false, nil
	}
	return item, err == nil, err
}

func (r *SQLRepository) ListEvents(ctx context.Context, limit, offset int, userID int64) ([]EventRecord, int, error) {
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM risk_events WHERE ($1=0 OR user_id=$1)`, userID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.QueryContext(ctx, eventSelect+` WHERE ($1=0 OR user_id=$1) ORDER BY occurred_at DESC LIMIT $2 OFFSET $3`, userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]EventRecord, 0)
	for rows.Next() {
		event, err := scanEventRows(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, event)
	}
	return items, total, rows.Err()
}

func (r *SQLRepository) InsertAudit(ctx context.Context, audit AuditRecord) error {
	metadata, _ := json.Marshal(audit.Metadata)
	_, err := r.db.ExecContext(ctx, `INSERT INTO risk_audit_logs(audit_key,actor_id,action,target_type,target_id,result,reason,metadata,created_at) VALUES(NULLIF($1,''),$2,$3,$4,$5,$6,$7,$8,COALESCE(NULLIF($9,'')::timestamptz,NOW())) ON CONFLICT DO NOTHING`, audit.AuditKey, audit.ActorID, audit.Action, audit.TargetType, audit.TargetID, audit.Result, audit.Reason, metadata, audit.CreatedAt)
	return err
}

func (r *SQLRepository) SetSubjectPending(ctx context.Context, userID int64, pending bool) error {
	if userID <= 0 {
		return nil
	}
	_, err := r.db.ExecContext(ctx, `UPDATE risk_subjects SET pending=$2,updated_at=NOW() WHERE user_id=$1`, userID, pending)
	return err
}

func (r *SQLRepository) ListAudit(ctx context.Context, limit, offset int, action string, targetUserID int64, result string) ([]AuditRecord, int, error) {
	return r.ListAuditFiltered(ctx, limit, offset, AuditFilter{Action: action, TargetUserID: targetUserID, Result: result})
}

func (r *SQLRepository) ListAuditFiltered(ctx context.Context, limit, offset int, filter AuditFilter) ([]AuditRecord, int, error) {
	where := []string{"1=1"}
	args := []any{}
	index := 1
	if filter.Category != "" {
		actions, ok := auditActionsForCategory(filter.Category)
		if !ok {
			return nil, 0, errors.New("invalid audit category")
		}
		where = append(where, fmt.Sprintf("action=ANY($%d::text[])", index))
		args = append(args, pq.Array(actions))
		index++
	}
	if filter.Action != "" {
		where = append(where, fmt.Sprintf("action=$%d", index))
		args = append(args, filter.Action)
		index++
	}
	if filter.TargetUserID > 0 {
		where = append(where, fmt.Sprintf("target_type='user' AND target_id=$%d", index))
		args = append(args, formatUserID(filter.TargetUserID))
		index++
	}
	if filter.Target != "" {
		where = append(where, fmt.Sprintf("(target_type ILIKE $%d OR target_id ILIKE $%d)", index, index))
		args = append(args, "%"+filter.Target+"%")
		index++
	}
	if filter.ActorID > 0 {
		where = append(where, fmt.Sprintf("actor_id=$%d", index))
		args = append(args, filter.ActorID)
		index++
	}
	if filter.Result != "" {
		where = append(where, fmt.Sprintf("result=$%d", index))
		args = append(args, filter.Result)
		index++
	}
	if !filter.From.IsZero() {
		where = append(where, fmt.Sprintf("created_at >= $%d", index))
		args = append(args, filter.From)
		index++
	}
	if !filter.To.IsZero() {
		where = append(where, fmt.Sprintf("created_at <= $%d", index))
		args = append(args, filter.To)
		index++
	}
	sortColumn := "created_at"
	if filter.SortBy == "result" {
		sortColumn = "result"
	} else if filter.SortBy == "target" {
		sortColumn = "CASE WHEN target_type='user' AND target_id ~ '^[0-9]+$' THEN '0:' || LPAD(target_id, 20, '0') ELSE '1:' || target_type || ':' || target_id END"
	}
	sortDirection := "DESC"
	if strings.EqualFold(filter.SortOrder, "asc") {
		sortDirection = "ASC"
	}
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM risk_audit_logs WHERE `+strings.Join(where, " AND "), args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id,actor_id,action,target_type,target_id,result,reason,metadata,to_char(created_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.MS"Z"') FROM risk_audit_logs WHERE `+strings.Join(where, " AND ")+fmt.Sprintf(" ORDER BY %s %s LIMIT $%d OFFSET $%d", sortColumn, sortDirection, index, index+1), append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]AuditRecord, 0)
	for rows.Next() {
		var item AuditRecord
		var metadata []byte
		if err := rows.Scan(&item.ID, &item.ActorID, &item.Action, &item.TargetType, &item.TargetID, &item.Result, &item.Reason, &metadata, &item.CreatedAt); err != nil {
			return nil, 0, err
		}
		_ = json.Unmarshal(metadata, &item.Metadata)
		enrichAuditRecord(&item)
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (r *SQLRepository) Overview(ctx context.Context, since time.Time) (RiskOverview, error) {
	var result RiskOverview
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*),COUNT(*) FILTER (WHERE decision='review'),COALESCE(MAX(to_char(occurred_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.MS"Z"')),'') FROM risk_events WHERE occurred_at >= $1`, since).Scan(&result.Events24H, &result.ReviewRate, &result.LastEventAt); err != nil {
		return result, err
	}
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM risk_subjects WHERE risk_level IN ('high','critical')`).Scan(&result.HighRiskSubjects); err != nil {
		return result, err
	}
	return result, nil
}

const eventSelect = `SELECT id,event_key,event_type,identity_version,user_id,subject_id,username_snapshot,account_status_snapshot,email_hash,ip_hash,device_hash,risk_type,error_code,reason,endpoint,model,http_status,evidence,decision,score,risk_level,rule_codes,to_char(occurred_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.MS"Z"') FROM risk_events`

type scanner interface{ Scan(...any) error }

func scanEvent(row scanner) (EventRecord, error)     { return scanEventValues(row) }
func scanEventRows(row scanner) (EventRecord, error) { return scanEventValues(row) }
func scanEventValues(row scanner) (EventRecord, error) {
	var event EventRecord
	var evidence, rules []byte
	err := row.Scan(&event.ID, &event.EventKey, &event.EventType, &event.IdentityVersion, &event.UserID, &event.SubjectID, &event.UsernameSnapshot, &event.AccountStatusSnapshot, &event.EmailHash, &event.IPHash, &event.DeviceHash, &event.RiskType, &event.ErrorCode, &event.Reason, &event.Endpoint, &event.Model, &event.HTTPStatus, &evidence, &event.Decision, &event.Score, &event.RiskLevel, &rules, &event.OccurredAt)
	if err != nil {
		return event, err
	}
	_ = json.Unmarshal(evidence, &event.Evidence)
	_ = json.Unmarshal(rules, &event.RuleCodes)
	return event, nil
}
