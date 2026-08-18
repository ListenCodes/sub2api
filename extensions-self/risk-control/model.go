package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var ErrRuleRevisionConflict = errors.New("rule revision conflict")
var ErrRuleCodeConflict = errors.New("rule code already exists")

type Rule struct {
	ID            int64    `json:"id"`
	Code          string   `json:"code"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	EventTypes    []string `json:"event_types"`
	CountStrategy string   `json:"count_strategy"`
	Enabled       bool     `json:"enabled"`
	WindowSeconds int      `json:"window_seconds"`
	Threshold     int      `json:"threshold"`
	Score         int      `json:"score"`
	RiskLevel     string   `json:"risk_level"`
	Action        string   `json:"action"`
	Revision      int      `json:"revision"`
}

type EventRecord struct {
	ID                    int64          `json:"id"`
	EventKey              string         `json:"event_key"`
	EventType             string         `json:"event_type"`
	IdentityVersion       string         `json:"identity_version"`
	UserID                int64          `json:"user_id,omitempty"`
	SubjectID             string         `json:"subject_id,omitempty"`
	UsernameSnapshot      string         `json:"username,omitempty"`
	AccountStatusSnapshot string         `json:"account_status,omitempty"`
	EmailHash             string         `json:"email_hash,omitempty"`
	IPHash                string         `json:"ip_hash,omitempty"`
	DeviceHash            string         `json:"device_hash,omitempty"`
	RiskType              string         `json:"risk_type,omitempty"`
	ErrorCode             string         `json:"error_code,omitempty"`
	Reason                string         `json:"reason,omitempty"`
	Endpoint              string         `json:"endpoint,omitempty"`
	Model                 string         `json:"model,omitempty"`
	HTTPStatus            int            `json:"http_status,omitempty"`
	Evidence              map[string]any `json:"evidence,omitempty"`
	Decision              string         `json:"decision"`
	Score                 int            `json:"score"`
	RiskLevel             string         `json:"risk_level"`
	RuleCodes             []string       `json:"rule_codes,omitempty"`
	OccurredAt            string         `json:"occurred_at"`
}

type RiskSubject struct {
	ID            int64  `json:"id"`
	UserID        int64  `json:"user_id"`
	Username      string `json:"username"`
	EmailHash     string `json:"email_hash"`
	AccountStatus string `json:"account_status"`
	RiskType      string `json:"risk_type"`
	RiskLevel     string `json:"risk_level"`
	Score         int    `json:"score"`
	Reason        string `json:"reason"`
	EventCount    int    `json:"event_count"`
	IPCount       int    `json:"ip_count"`
	DeviceCount   int    `json:"device_count"`
	LastAction    string `json:"last_action"`
	Pending       bool   `json:"pending"`
	LastEventAt   string `json:"last_event_at"`
}

type RiskIndexItem struct {
	UserID             int64  `json:"id"`
	RiskType           string `json:"risk_type"`
	RiskLevel          string `json:"risk_level"`
	Score              int    `json:"score"`
	Reason             string `json:"reason"`
	EventCount         int    `json:"event_count"`
	IPCount            int    `json:"ip_count"`
	DeviceCount        int    `json:"device_count"`
	LastAction         string `json:"last_action"`
	Pending            bool   `json:"pending"`
	LastEventAt        string `json:"last_event_at"`
	ProcessingStatus   string `json:"processing_status,omitempty"`
	CaseID             int64  `json:"case_id,omitempty"`
	CaseStatus         string `json:"case_status,omitempty"`
	AssigneeID         int64  `json:"assignee_id,omitempty"`
	EvidenceStrength   string `json:"evidence_strength,omitempty"`
	DecisionID         string `json:"decision_id,omitempty"`
	HistoricalMaxScore int    `json:"historical_max_score,omitempty"`
}

type RiskIndexFilter struct {
	RiskType        string
	RiskLevel       string
	MinScore        int
	MaxScore        int
	ProcessingState string
	SortBy          string
	SortOrder       string
	UserIDs         []int64
	OmitAllUserIDs  bool
}

type AuditRecord struct {
	ID            int64          `json:"id"`
	AuditKey      string         `json:"audit_key,omitempty"`
	ActorID       int64          `json:"actor_id"`
	Action        string         `json:"action"`
	TargetType    string         `json:"target_type"`
	TargetID      string         `json:"target_id"`
	Result        string         `json:"result"`
	Reason        string         `json:"reason"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	FailureReason string         `json:"failure_reason,omitempty"`
	BatchID       string         `json:"batch_id,omitempty"`
	RequestID     string         `json:"request_id,omitempty"`
	CreatedAt     string         `json:"created_at"`
}

type AuditFilter struct {
	Category     string
	Action       string
	TargetUserID int64
	Target       string
	ActorID      int64
	Result       string
	From         time.Time
	To           time.Time
	SortBy       string
	SortOrder    string
}

var auditCategoryActions = map[string]map[string]struct{}{
	"security": {
		"ban": {}, "unban": {}, "auto_ban": {}, "mark_processed": {},
		"claim_risk_review_case": {}, "review_risk_case": {}, "label_shared_network": {},
	},
	"rules": {
		"create_rule": {}, "update_rule": {}, "rule_test": {}, "disable_identity_rule": {},
		"purge_legacy_v1": {}, "identity_rebuild_dry_run": {}, "identity_rebuild": {},
	},
	"sensitive": {"view_identity_detail": {}},
}

func auditActionsForCategory(category string) ([]string, bool) {
	actions, ok := auditCategoryActions[category]
	if !ok {
		return nil, false
	}
	result := make([]string, 0, len(actions))
	for action := range actions {
		result = append(result, action)
	}
	sort.Strings(result)
	return result, true
}

type RiskOverview struct {
	OpenCases        int    `json:"open_cases"`
	Events24H        int    `json:"events_24h"`
	HighRiskSubjects int    `json:"high_risk_subjects"`
	ReviewRate       int    `json:"review_rate"`
	Mode             string `json:"mode"`
	LastEventAt      string `json:"last_event_at,omitempty"`
}

type RiskRepository interface {
	InsertEvent(context.Context, EventRecord) (EventRecord, bool, error)
	GetEventByKey(context.Context, string) (EventRecord, bool, error)
	CountRecent(context.Context, int64, string, string, string, string, string, time.Time) (int, error)
	ListRules(context.Context) ([]Rule, error)
	CreateRule(context.Context, Rule) (Rule, error)
	UpdateRule(context.Context, string, int, Rule) (Rule, error)
	UpsertSubject(context.Context, EventRecord) error
	ListSubjects(context.Context, int, int, string, string, []int64) ([]RiskSubject, int, error)
	ListRiskIndex(context.Context, RiskIndexFilter, int, int) ([]RiskIndexItem, []int64, int, error)
	GetSubject(context.Context, int64) (RiskSubject, bool, error)
	ListEvents(context.Context, int, int, int64) ([]EventRecord, int, error)
	InsertAudit(context.Context, AuditRecord) error
	SetSubjectPending(context.Context, int64, bool) error
	ListAudit(context.Context, int, int, string, int64, string) ([]AuditRecord, int, error)
	ListAuditFiltered(context.Context, int, int, AuditFilter) ([]AuditRecord, int, error)
	Overview(context.Context, time.Time) (RiskOverview, error)
}

type MemoryRuleStore struct {
	mu    sync.RWMutex
	rules map[string]Rule
}

func newMemoryRuleStore(seed []Rule) *MemoryRuleStore {
	store := &MemoryRuleStore{rules: make(map[string]Rule, len(seed))}
	for _, rule := range seed {
		store.rules[rule.Code] = rule
	}
	return store
}

func (s *MemoryRuleStore) UpdateRule(code string, expectedRevision int, update Rule) (Rule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.rules[code]
	if !ok || current.Revision != expectedRevision {
		return Rule{}, ErrRuleRevisionConflict
	}
	update.ID = current.ID
	update.Code = code
	update.Revision = current.Revision + 1
	update.CountStrategy = normalizeCountStrategy(update.CountStrategy)
	s.rules[code] = update
	return update, nil
}

type MemoryRepository struct {
	mu            sync.RWMutex
	rules         map[string]Rule
	events        []EventRecord
	eventByKey    map[string]EventRecord
	subjects      map[int64]RiskSubject
	userIPs       map[int64]map[string]struct{}
	userDevices   map[int64]map[string]struct{}
	audits        []AuditRecord
	subjectEvents map[int64]map[string]struct{}
	auditKeys     map[string]struct{}
	nextEventID   int64
	nextAuditID   int64
	nextRuleID    int64
}

func NewMemoryRepository(seed []Rule) *MemoryRepository {
	rules := make(map[string]Rule, len(seed))
	nextRuleID := int64(1)
	for _, rule := range seed {
		rules[rule.Code] = rule
		if rule.ID >= nextRuleID {
			nextRuleID = rule.ID + 1
		}
	}
	return &MemoryRepository{
		rules: rules, eventByKey: make(map[string]EventRecord), subjects: make(map[int64]RiskSubject),
		userIPs: make(map[int64]map[string]struct{}), userDevices: make(map[int64]map[string]struct{}),
		subjectEvents: make(map[int64]map[string]struct{}), auditKeys: make(map[string]struct{}),
		nextEventID: 1, nextAuditID: 1, nextRuleID: nextRuleID,
	}
}

func (r *MemoryRepository) InsertEvent(_ context.Context, event EventRecord) (EventRecord, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.eventByKey[event.EventKey]; ok {
		return existing, true, nil
	}
	if event.ID == 0 {
		event.ID = r.nextEventID
		r.nextEventID++
	}
	r.events = append(r.events, event)
	r.eventByKey[event.EventKey] = event
	return event, false, nil
}

func (r *MemoryRepository) GetEventByKey(_ context.Context, key string) (EventRecord, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	event, ok := r.eventByKey[key]
	return event, ok, nil
}

func (r *MemoryRepository) CountRecent(_ context.Context, userID int64, subjectID, ipHash, deviceHash, eventType, countStrategy string, since time.Time) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	count := 0
	distinctSubjects := make(map[string]struct{})
	for _, event := range r.events {
		occurred, err := parseTime(event.OccurredAt)
		if event.EventType != eventType || err != nil || occurred.Before(since) {
			continue
		}
		switch normalizeCountStrategy(countStrategy) {
		case countStrategyEmailSubjectEvents:
			if subjectID != "" && event.SubjectID == subjectID {
				count++
			}
		case countStrategyIPDistinctSuccessUsers:
			if ipHash != "" && event.IPHash == ipHash && event.UserID > 0 && event.UserID != userID {
				distinctSubjects[formatUserID(event.UserID)] = struct{}{}
			}
		case countStrategyBrowserDistinctSuccessUsers, countStrategyAPIClientDistinctUsers:
			if deviceHash != "" && event.DeviceHash == deviceHash && event.UserID > 0 && event.UserID != userID {
				distinctSubjects[formatUserID(event.UserID)] = struct{}{}
			}
		case countStrategyIPBrowserCooccurrence:
			if ipHash != "" && deviceHash != "" && event.IPHash == ipHash && event.DeviceHash == deviceHash && event.UserID > 0 && event.UserID != userID {
				distinctSubjects[formatUserID(event.UserID)] = struct{}{}
			}
		default:
			if userID > 0 && event.UserID == userID {
				count++
			}
		}
	}
	if strategy := normalizeCountStrategy(countStrategy); strategy == countStrategyIPDistinctSuccessUsers || strategy == countStrategyBrowserDistinctSuccessUsers || strategy == countStrategyAPIClientDistinctUsers || strategy == countStrategyIPBrowserCooccurrence {
		return len(distinctSubjects), nil
	}
	return count, nil
}

func (r *MemoryRepository) ListRules(_ context.Context) ([]Rule, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Rule, 0, len(r.rules))
	for _, rule := range r.rules {
		result = append(result, rule)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Code < result[j].Code })
	return result, nil
}

func (r *MemoryRepository) CreateRule(_ context.Context, rule Rule) (Rule, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.rules[rule.Code]; exists {
		return Rule{}, ErrRuleCodeConflict
	}
	rule.ID = r.nextRuleID
	r.nextRuleID++
	rule.Revision = 1
	rule.CountStrategy = normalizeCountStrategy(rule.CountStrategy)
	r.rules[rule.Code] = rule
	return rule, nil
}

func (r *MemoryRepository) UpdateRule(_ context.Context, code string, expectedRevision int, update Rule) (Rule, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.rules[code]
	if !ok || current.Revision != expectedRevision {
		return Rule{}, ErrRuleRevisionConflict
	}
	update.ID, update.Code, update.Revision = current.ID, code, current.Revision+1
	update.CountStrategy = normalizeCountStrategy(update.CountStrategy)
	r.rules[code] = update
	return update, nil
}

func (r *MemoryRepository) UpsertSubject(_ context.Context, event EventRecord) error {
	if event.UserID <= 0 {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if event.EventKey != "" {
		if r.subjectEvents[event.UserID] == nil {
			r.subjectEvents[event.UserID] = make(map[string]struct{})
		}
		if _, exists := r.subjectEvents[event.UserID][event.EventKey]; exists {
			return nil
		}
		r.subjectEvents[event.UserID][event.EventKey] = struct{}{}
	}
	subject := r.subjects[event.UserID]
	subject.ID = event.UserID
	subject.UserID = event.UserID
	subject.Username = event.UsernameSnapshot
	subject.EmailHash = event.EmailHash
	subject.AccountStatus = event.AccountStatusSnapshot
	if shouldReplaceRiskSignal(subject, event) {
		subject.RiskType = event.RiskType
		subject.RiskLevel = event.RiskLevel
		subject.Score = event.Score
		subject.Reason = event.Reason
		subject.LastAction = event.Decision
	}
	subject.Pending = subject.Pending || event.Decision == "review"
	subject.EventCount++
	subject.LastEventAt = event.OccurredAt
	if r.userIPs[event.UserID] == nil {
		r.userIPs[event.UserID] = make(map[string]struct{})
	}
	if event.IPHash != "" {
		r.userIPs[event.UserID][event.IPHash] = struct{}{}
	}
	if r.userDevices[event.UserID] == nil {
		r.userDevices[event.UserID] = make(map[string]struct{})
	}
	if event.DeviceHash != "" {
		r.userDevices[event.UserID][event.DeviceHash] = struct{}{}
	}
	subject.IPCount = len(r.userIPs[event.UserID])
	subject.DeviceCount = len(r.userDevices[event.UserID])
	r.subjects[event.UserID] = subject
	return nil
}

func shouldReplaceRiskSignal(subject RiskSubject, event EventRecord) bool {
	if subject.RiskType == "" || subject.RiskLevel == "" || subject.RiskLevel == "none" {
		return true
	}
	if event.Score != subject.Score {
		return event.Score > subject.Score
	}
	if riskLevelRank(event.RiskLevel) != riskLevelRank(subject.RiskLevel) {
		return riskLevelRank(event.RiskLevel) > riskLevelRank(subject.RiskLevel)
	}
	return riskActionRank(event.Decision) > riskActionRank(subject.LastAction)
}

func (r *MemoryRepository) ListSubjects(_ context.Context, limit, offset int, riskType, riskLevel string, userIDs []int64) ([]RiskSubject, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	allowedUsers := make(map[int64]struct{}, len(userIDs))
	for _, userID := range userIDs {
		allowedUsers[userID] = struct{}{}
	}
	items := make([]RiskSubject, 0, len(r.subjects))
	for _, subject := range r.subjects {
		if len(userIDs) > 0 {
			if _, ok := allowedUsers[subject.UserID]; !ok {
				continue
			}
		}
		if riskType != "" && subject.RiskType != riskType {
			continue
		}
		if riskLevel != "" && subject.RiskLevel != riskLevel {
			continue
		}
		items = append(items, subject)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Score > items[j].Score })
	return sliceSubjects(items, limit, offset), len(items), nil
}

func (r *MemoryRepository) ListRiskIndex(_ context.Context, filter RiskIndexFilter, limit, offset int) ([]RiskIndexItem, []int64, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	allIDs := make([]int64, 0, len(r.subjects))
	items := make([]RiskIndexItem, 0, len(r.subjects))
	requested := make(map[int64]bool, len(filter.UserIDs))
	for _, userID := range filter.UserIDs {
		requested[userID] = true
	}
	for _, subject := range r.subjects {
		if subject.Score <= 0 {
			continue
		}
		if !filter.OmitAllUserIDs {
			allIDs = append(allIDs, subject.UserID)
		}
		if len(requested) > 0 && !requested[subject.UserID] {
			continue
		}
		level := identityRiskLevel(subject.Score)
		state := ""
		if subject.Pending {
			state = "pending"
		}
		if filter.RiskType != "" && subject.RiskType != filter.RiskType ||
			filter.RiskLevel != "" && level != filter.RiskLevel ||
			filter.MinScore >= 0 && subject.Score < filter.MinScore ||
			filter.MaxScore >= 0 && subject.Score > filter.MaxScore ||
			filter.ProcessingState != "" && state != filter.ProcessingState {
			continue
		}
		items = append(items, RiskIndexItem{
			UserID: subject.UserID, RiskType: subject.RiskType, RiskLevel: level, Score: subject.Score,
			Reason: subject.Reason, EventCount: subject.EventCount, IPCount: subject.IPCount, DeviceCount: subject.DeviceCount,
			LastAction: subject.LastAction, Pending: subject.Pending, LastEventAt: subject.LastEventAt, ProcessingStatus: state,
		})
	}
	sort.Slice(allIDs, func(i, j int) bool { return allIDs[i] < allIDs[j] })
	sortRiskIndexItems(items, filter.SortBy, filter.SortOrder)
	total := len(items)
	if offset >= total {
		return []RiskIndexItem{}, allIDs, total, nil
	}
	end := minInt(offset+limit, total)
	return append([]RiskIndexItem(nil), items[offset:end]...), allIDs, total, nil
}

func sortRiskIndexItems(items []RiskIndexItem, sortBy, sortOrder string) {
	descending := !strings.EqualFold(strings.TrimSpace(sortOrder), "asc")
	sort.SliceStable(items, func(i, j int) bool {
		left, right := items[i], items[j]
		comparison := 0
		switch strings.TrimSpace(sortBy) {
		case "last_event_at":
			comparison = strings.Compare(left.LastEventAt, right.LastEventAt)
		case "event_count":
			comparison = compareInt(left.EventCount, right.EventCount)
		default:
			comparison = compareInt(left.Score, right.Score)
		}
		if comparison != 0 {
			if descending {
				return comparison > 0
			}
			return comparison < 0
		}
		if left.LastEventAt != right.LastEventAt {
			return left.LastEventAt > right.LastEventAt
		}
		return left.UserID < right.UserID
	})
}

func compareInt(left, right int) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func (r *MemoryRepository) GetSubject(_ context.Context, userID int64) (RiskSubject, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	subject, ok := r.subjects[userID]
	return subject, ok, nil
}

func (r *MemoryRepository) ListEvents(_ context.Context, limit, offset int, userID int64) ([]EventRecord, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]EventRecord, 0, len(r.events))
	for i := len(r.events) - 1; i >= 0; i-- {
		if userID == 0 || r.events[i].UserID == userID {
			items = append(items, r.events[i])
		}
	}
	return sliceEvents(items, limit, offset), len(items), nil
}

func (r *MemoryRepository) InsertAudit(_ context.Context, audit AuditRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	enrichAuditRecord(&audit)
	if audit.AuditKey != "" {
		if _, exists := r.auditKeys[audit.AuditKey]; exists {
			return nil
		}
		r.auditKeys[audit.AuditKey] = struct{}{}
	}
	if audit.ID == 0 {
		audit.ID = r.nextAuditID
		r.nextAuditID++
	}
	r.audits = append(r.audits, audit)
	return nil
}

func (r *MemoryRepository) SetSubjectPending(_ context.Context, userID int64, pending bool) error {
	if userID <= 0 {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	subject, exists := r.subjects[userID]
	if !exists {
		return nil
	}
	subject.Pending = pending
	r.subjects[userID] = subject
	return nil
}

func (r *MemoryRepository) ListAudit(_ context.Context, limit, offset int, action string, targetUserID int64, result string) ([]AuditRecord, int, error) {
	return r.ListAuditFiltered(context.Background(), limit, offset, AuditFilter{Action: action, TargetUserID: targetUserID, Result: result})
}

func (r *MemoryRepository) ListAuditFiltered(_ context.Context, limit, offset int, filter AuditFilter) ([]AuditRecord, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]AuditRecord, 0, len(r.audits))
	for i := len(r.audits) - 1; i >= 0; i-- {
		audit := r.audits[i]
		enrichAuditRecord(&audit)
		if actions, ok := auditCategoryActions[filter.Category]; ok {
			if _, included := actions[audit.Action]; !included {
				continue
			}
		}
		if filter.Action != "" && audit.Action != filter.Action || filter.Result != "" && audit.Result != filter.Result {
			continue
		}
		if filter.TargetUserID > 0 && audit.TargetID != formatUserID(filter.TargetUserID) {
			continue
		}
		if filter.ActorID > 0 && audit.ActorID != filter.ActorID {
			continue
		}
		if filter.Target != "" && !strings.Contains(strings.ToLower(audit.TargetID), strings.ToLower(filter.Target)) && !strings.Contains(strings.ToLower(audit.TargetType), strings.ToLower(filter.Target)) {
			continue
		}
		created, err := parseTime(audit.CreatedAt)
		if !filter.From.IsZero() && (err != nil || created.Before(filter.From)) {
			continue
		}
		if !filter.To.IsZero() && (err != nil || created.After(filter.To)) {
			continue
		}
		items = append(items, audit)
	}
	sort.SliceStable(items, func(i, j int) bool {
		left, right := auditSortValue(items[i], filter.SortBy), auditSortValue(items[j], filter.SortBy)
		if left == right {
			return items[i].ID > items[j].ID
		}
		if strings.EqualFold(filter.SortOrder, "asc") {
			return left < right
		}
		return left > right
	})
	return sliceAudits(items, limit, offset), len(items), nil
}

func auditSortValue(audit AuditRecord, sortBy string) string {
	switch sortBy {
	case "result":
		return audit.Result
	case "target":
		if audit.TargetType == "user" {
			if userID, err := strconv.ParseInt(audit.TargetID, 10, 64); err == nil {
				return fmt.Sprintf("0:%020d", userID)
			}
		}
		return "1:" + audit.TargetType + ":" + audit.TargetID
	default:
		return audit.CreatedAt
	}
}

func enrichAuditRecord(audit *AuditRecord) {
	if audit == nil || audit.Metadata == nil {
		return
	}
	if audit.FailureReason == "" {
		audit.FailureReason, _ = audit.Metadata["failure_reason"].(string)
	}
	if audit.BatchID == "" {
		audit.BatchID, _ = audit.Metadata["batch_id"].(string)
	}
	if audit.RequestID == "" {
		audit.RequestID, _ = audit.Metadata["request_id"].(string)
	}
}

func (r *MemoryRepository) Overview(_ context.Context, since time.Time) (RiskOverview, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := RiskOverview{Mode: "shadow"}
	for _, event := range r.events {
		occurred, err := parseTime(event.OccurredAt)
		if err != nil || occurred.Before(since) {
			continue
		}
		result.Events24H++
		if event.Decision == "review" {
			result.ReviewRate++
		}
		if event.RiskLevel == "high" || event.RiskLevel == "critical" {
			result.HighRiskSubjects++
		}
		if result.LastEventAt == "" || event.OccurredAt > result.LastEventAt {
			result.LastEventAt = event.OccurredAt
		}
	}
	return result, nil
}

func parseTime(value string) (time.Time, error) { return time.Parse(time.RFC3339Nano, value) }
func formatUserID(userID int64) string          { return strconv.FormatInt(userID, 10) }

func sliceSubjects(items []RiskSubject, limit, offset int) []RiskSubject {
	if offset >= len(items) {
		return []RiskSubject{}
	}
	end := minInt(offset+limit, len(items))
	return items[offset:end]
}
func sliceEvents(items []EventRecord, limit, offset int) []EventRecord {
	if offset >= len(items) {
		return []EventRecord{}
	}
	end := minInt(offset+limit, len(items))
	return items[offset:end]
}
func sliceAudits(items []AuditRecord, limit, offset int) []AuditRecord {
	if offset >= len(items) {
		return []AuditRecord{}
	}
	end := minInt(offset+limit, len(items))
	return items[offset:end]
}
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
