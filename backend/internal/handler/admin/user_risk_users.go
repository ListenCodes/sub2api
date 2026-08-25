package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type riskCaseListItem struct {
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
	CreatedBy          int64  `json:"created_by"`
	ReviewDueAt        string `json:"review_due_at"`
	ObservationGoal    string `json:"observation_goal"`
	ResolutionReason   string `json:"resolution_reason"`
	Revision           int    `json:"revision"`
	LastActivityAt     string `json:"last_activity_at"`
	LastHitAt          string `json:"last_hit_at"`
}

type riskCaseListPage struct {
	Items    []riskCaseListItem `json:"items"`
	Total    int                `json:"total"`
	Page     int                `json:"page"`
	PageSize int                `json:"page_size"`
}

type riskIdentitySummaryPage struct {
	Items []map[string]any `json:"items"`
}

type riskIndexListItem struct {
	ID                 int64  `json:"id"`
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
	ProcessingStatus   string `json:"processing_status"`
	CaseID             int64  `json:"case_id"`
	CaseStatus         string `json:"case_status"`
	AssigneeID         int64  `json:"assignee_id"`
	EvidenceStrength   string `json:"evidence_strength"`
	DecisionID         string `json:"decision_id"`
	HistoricalMaxScore int    `json:"historical_max_score"`
}

type riskIndexListPage struct {
	Items       []riskIndexListItem `json:"items"`
	RiskUserIDs []int64             `json:"risk_user_ids"`
	Total       int                 `json:"total"`
}

// ListUserRiskUsers is the custom-owned aggregation boundary. It keeps account
// data in the main service while cases, current scores and evidence stay in the
// extension, and returns one already-paginated response to the browser.
func (h *CustomUserHandler) ListUserRiskUsers(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	if pageSize > 100 {
		pageSize = 100
	}
	view := strings.TrimSpace(c.DefaultQuery("view", "unassigned"))
	if view == "users" || view == "all" {
		h.listAllUserRiskUsers(c, page, pageSize)
		return
	}
	query := url.Values{}
	query.Set("view", view)
	copyRiskQuery(query, c, "risk_type", "risk_level", "processing_status", "risk_only", "min_score", "max_score", "sort_by", "sort_order")
	accountFilter := strings.TrimSpace(c.Query("search")) != "" || strings.TrimSpace(c.Query("status")) != ""
	var cases riskCaseListPage
	if accountFilter {
		var ok bool
		cases, ok = h.listAllRiskCases(c, query)
		if !ok {
			return
		}
	} else {
		query.Set("page", strconv.Itoa(page))
		query.Set("limit", strconv.Itoa(pageSize))
		var ok bool
		cases, ok = h.listRiskCasePage(c, query)
		if !ok {
			return
		}
	}
	accounts, available := h.riskIdentityAccounts(c, riskCaseUserIDs(cases.Items))
	if accountFilter && !available {
		response.Error(c, http.StatusServiceUnavailable, "Account lookup is unavailable")
		return
	}
	items := make([]map[string]any, 0, len(cases.Items))
	for _, reviewCase := range cases.Items {
		account := accounts[reviewCase.UserID]
		if !riskAccountMatches(account, available, c.Query("search"), c.Query("status")) {
			continue
		}
		row := riskAccountRow(reviewCase.UserID, account, available)
		row["risk_type"] = reviewCase.PrimarySignal
		row["risk_level"] = riskLevelForScore(reviewCase.CurrentScore)
		row["risk_score"] = reviewCase.CurrentScore
		row["risk_reason"] = reviewCase.PrimarySignal
		row["last_event_at"] = reviewCase.LastHitAt
		row["last_risk_at"] = reviewCase.LastHitAt
		row["pending"] = reviewCase.Status == "pending" || reviewCase.Status == "in_review"
		row["processing_status"] = reviewCase.Status
		row["case_id"] = reviewCase.ID
		row["case_status"] = reviewCase.Status
		row["assignee_id"] = reviewCase.AssigneeID
		row["created_by"] = reviewCase.CreatedBy
		row["review_due_at"] = reviewCase.ReviewDueAt
		row["observation_goal"] = reviewCase.ObservationGoal
		row["resolution_reason"] = reviewCase.ResolutionReason
		row["case_revision"] = reviewCase.Revision
		row["last_activity_at"] = reviewCase.LastActivityAt
		row["evidence_strength"] = reviewCase.EvidenceStrength
		row["decision_id"] = reviewCase.DecisionID
		row["historical_max_score"] = reviewCase.HistoricalMaxScore
		items = append(items, row)
	}
	total := cases.Total
	if accountFilter {
		total = len(items)
		start := (page - 1) * pageSize
		if start > len(items) {
			start = len(items)
		}
		end := start + pageSize
		if end > len(items) {
			end = len(items)
		}
		items = items[start:end]
	}
	h.attachRiskIdentitySummaries(c, items)
	response.Paginated(c, items, int64(total), page, pageSize)
}

func hasRiskCaseFilters(c *gin.Context) bool {
	for _, key := range []string{"risk_type", "risk_level", "processing_status", "min_score", "max_score"} {
		if strings.TrimSpace(c.Query(key)) != "" {
			return true
		}
	}
	return strings.EqualFold(strings.TrimSpace(c.Query("risk_only")), "true")
}

func (h *CustomUserHandler) listRiskCasePage(c *gin.Context, query url.Values) (riskCaseListPage, bool) {
	body, status, err := h.proxyRiskIdentity(c, http.MethodGet, "/api/v1/admin/review-cases?"+query.Encode(), nil)
	if err != nil {
		return riskCaseListPage{}, false
	}
	if status < 200 || status >= 300 {
		c.Data(status, "application/json", body)
		return riskCaseListPage{}, false
	}
	var cases riskCaseListPage
	if err := json.Unmarshal(body, &cases); err != nil {
		response.Error(c, http.StatusBadGateway, "Risk case response is invalid")
		return riskCaseListPage{}, false
	}
	return cases, true
}

func (h *CustomUserHandler) listAllRiskCases(c *gin.Context, base url.Values) (riskCaseListPage, bool) {
	const batchSize = 100
	all := riskCaseListPage{Page: 1, PageSize: batchSize}
	for page := 1; ; page++ {
		query := cloneURLValues(base)
		query.Set("page", strconv.Itoa(page))
		query.Set("limit", strconv.Itoa(batchSize))
		current, ok := h.listRiskCasePage(c, query)
		if !ok {
			return riskCaseListPage{}, false
		}
		if page == 1 {
			all.Total = current.Total
		}
		all.Items = append(all.Items, current.Items...)
		if len(current.Items) == 0 || len(all.Items) >= current.Total {
			break
		}
	}
	return all, true
}

func cloneURLValues(values url.Values) url.Values {
	result := make(url.Values, len(values))
	for key, entries := range values {
		result[key] = append([]string(nil), entries...)
	}
	return result
}

func (h *CustomUserHandler) listAllUserRiskUsers(c *gin.Context, page, pageSize int) {
	search := strings.TrimSpace(c.Query("search"))
	if runes := []rune(search); len(runes) > 100 {
		search = string(runes[:100])
	}
	filters := service.UserListFilters{Search: search, Status: c.Query("status")}
	if strings.TrimSpace(c.Query("sort_by")) != "created_at" || hasRiskCaseFilters(c) {
		h.listAllUserRiskUsersFromIndex(c, page, pageSize, filters)
		return
	}
	users, total, err := h.adminService.ListUsers(c.Request.Context(), page, pageSize, filters, "created_at", normalizedSortOrder(c.Query("sort_order")))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	ids := make([]int64, 0, len(users))
	items := make([]map[string]any, 0, len(users))
	for index := range users {
		user := &users[index]
		ids = append(ids, user.ID)
		items = append(items, riskAccountRow(user.ID, identityAccountPayload(user), true))
	}
	if len(ids) > 0 && !h.attachRiskIndexForRows(c, items) {
		return
	}
	h.attachRiskIdentitySummaries(c, items)
	response.Paginated(c, items, total, page, pageSize)
}

func (h *CustomUserHandler) attachRiskIndexForRows(c *gin.Context, items []map[string]any) bool {
	ids := riskMapUserIDs(items)
	if len(ids) == 0 {
		return true
	}
	query := url.Values{
		"user_ids":        {joinRiskUserIDs(ids)},
		"include_all_ids": {"false"},
		"limit":           {strconv.Itoa(len(ids))},
	}
	body, status, err := h.proxyRiskIdentity(c, http.MethodGet, "/api/v1/admin/risk-index?"+query.Encode(), nil)
	if err != nil {
		return false
	}
	if status < 200 || status >= 300 {
		c.Data(status, "application/json", body)
		return false
	}
	var page riskIndexListPage
	if json.Unmarshal(body, &page) != nil {
		response.Error(c, http.StatusBadGateway, "Risk index response is invalid")
		return false
	}
	byID := make(map[int64]map[string]any, len(items))
	for _, item := range items {
		if id, ok := item["id"].(int64); ok {
			byID[id] = item
		}
	}
	for _, risk := range riskIndexRows(page.Items) {
		id, _ := risk["id"].(int64)
		row := byID[id]
		if row == nil {
			continue
		}
		for key, value := range risk {
			if key != "id" {
				row[key] = value
			}
		}
	}
	return true
}

func (h *CustomUserHandler) listAllUserRiskUsersFromIndex(c *gin.Context, page, pageSize int, filters service.UserListFilters) {
	baseQuery := url.Values{}
	copyRiskQuery(baseQuery, c, "risk_type", "risk_level", "processing_status", "risk_only", "min_score", "max_score", "sort_by", "sort_order")
	if baseQuery.Get("sort_by") == "" {
		baseQuery.Set("sort_by", "risk_score")
	}
	accountFiltered := strings.TrimSpace(filters.Search) != "" || strings.TrimSpace(filters.Status) != ""
	riskFiltered := hasRiskCaseFilters(c)
	if riskFiltered {
		baseQuery.Set("include_all_ids", "false")
	}
	meta, ok := h.riskIndexPage(c, baseQuery, 1, 0)
	if !ok {
		return
	}
	allRiskRows := []map[string]any(nil)
	if accountFiltered || strings.TrimSpace(c.Query("sort_by")) == "created_at" {
		pageQuery := cloneURLValues(baseQuery)
		pageQuery.Set("include_all_ids", "false")
		allRiskRows, ok = h.allRiskIndexRows(c, pageQuery, meta.Total)
		if !ok {
			return
		}
		accounts, available := h.riskIdentityAccounts(c, riskMapUserIDs(allRiskRows))
		if !available {
			response.Error(c, http.StatusServiceUnavailable, "Account lookup is unavailable")
			return
		}
		filtered := allRiskRows[:0]
		for _, row := range allRiskRows {
			id, _ := row["id"].(int64)
			account := accounts[id]
			if !riskAccountMatches(account, true, filters.Search, filters.Status) {
				continue
			}
			mergeRiskAccount(row, id, account, true)
			filtered = append(filtered, row)
		}
		allRiskRows = filtered
		if strings.TrimSpace(c.Query("sort_by")) == "created_at" {
			sort.SliceStable(allRiskRows, func(i, j int) bool {
				left, right := fmt.Sprint(allRiskRows[i]["created_at"]), fmt.Sprint(allRiskRows[j]["created_at"])
				if normalizedSortOrder(c.Query("sort_order")) == "asc" {
					return left < right
				}
				return left > right
			})
		}
	}
	riskTotal := meta.Total
	if allRiskRows != nil {
		riskTotal = len(allRiskRows)
	}
	includeNormal := !riskFiltered
	normalTotal := int64(0)
	var normalProbe []service.User
	if includeNormal {
		var err error
		normalProbe, normalTotal, err = h.listNormalRiskAccounts(c, filters, meta.RiskUserIDs, 0, pageSize, pageSize)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
	}
	globalOffset := (page - 1) * pageSize
	descending := normalizedSortOrder(c.Query("sort_order")) == "desc"
	items := make([]map[string]any, 0, pageSize)
	appendRisk := func(offset, count int) bool {
		if count <= 0 {
			return true
		}
		var rows []map[string]any
		if allRiskRows != nil {
			end := minIntValue(offset+count, len(allRiskRows))
			if offset < end {
				rows = append(rows, allRiskRows[offset:end]...)
			}
		} else {
			pageResult, loaded := h.riskIndexPage(c, baseQuery, count, offset)
			if !loaded {
				return false
			}
			rows = riskIndexRows(pageResult.Items)
			accounts, available := h.riskIdentityAccounts(c, riskMapUserIDs(rows))
			for _, row := range rows {
				id, _ := row["id"].(int64)
				mergeRiskAccount(row, id, accounts[id], available)
			}
		}
		items = append(items, rows...)
		return true
	}
	appendNormal := func(offset, count int) bool {
		if count <= 0 {
			return true
		}
		users := []service.User(nil)
		var err error
		if offset == 0 && count <= len(normalProbe) {
			users = normalProbe[:count]
		} else {
			users, _, err = h.listNormalRiskAccounts(c, filters, meta.RiskUserIDs, offset, count, pageSize)
		}
		if err != nil {
			response.ErrorFrom(c, err)
			return false
		}
		for index := range users {
			items = append(items, riskAccountRow(users[index].ID, identityAccountPayload(&users[index]), true))
		}
		return true
	}
	if !includeNormal {
		if !appendRisk(globalOffset, pageSize) {
			return
		}
	} else if descending {
		riskOffset := minIntValue(globalOffset, riskTotal)
		riskCount := minIntValue(pageSize, maxIntValue(riskTotal-globalOffset, 0))
		if !appendRisk(riskOffset, riskCount) || !appendNormal(maxIntValue(globalOffset-riskTotal, 0), pageSize-len(items)) {
			return
		}
	} else {
		normalCount := minIntValue(pageSize, maxIntValue(int(normalTotal)-globalOffset, 0))
		if !appendNormal(minIntValue(globalOffset, int(normalTotal)), normalCount) || !appendRisk(maxIntValue(globalOffset-int(normalTotal), 0), pageSize-len(items)) {
			return
		}
	}
	total := int64(riskTotal)
	if includeNormal {
		total += normalTotal
	}
	h.attachRiskIdentitySummaries(c, items)
	response.Paginated(c, items, total, page, pageSize)
}

func (h *CustomUserHandler) riskIndexPage(c *gin.Context, base url.Values, limit, offset int) (riskIndexListPage, bool) {
	query := cloneURLValues(base)
	query.Set("limit", strconv.Itoa(limit))
	query.Set("offset", strconv.Itoa(offset))
	body, status, err := h.proxyRiskIdentity(c, http.MethodGet, "/api/v1/admin/risk-index?"+query.Encode(), nil)
	if err != nil {
		return riskIndexListPage{}, false
	}
	if status < 200 || status >= 300 {
		c.Data(status, "application/json", body)
		return riskIndexListPage{}, false
	}
	var result riskIndexListPage
	if json.Unmarshal(body, &result) != nil {
		response.Error(c, http.StatusBadGateway, "Risk index response is invalid")
		return riskIndexListPage{}, false
	}
	return result, true
}

func (h *CustomUserHandler) allRiskIndexRows(c *gin.Context, base url.Values, total int) ([]map[string]any, bool) {
	const batchSize = 1000
	items := make([]map[string]any, 0, total)
	for offset := 0; offset < total; offset += batchSize {
		page, ok := h.riskIndexPage(c, base, minIntValue(batchSize, total-offset), offset)
		if !ok {
			return nil, false
		}
		items = append(items, riskIndexRows(page.Items)...)
	}
	return items, true
}

func (h *CustomUserHandler) listNormalRiskAccounts(c *gin.Context, filters service.UserListFilters, excluded []int64, offset, count, pageSize int) ([]service.User, int64, error) {
	filters.ExcludeIDs = append([]int64(nil), excluded...)
	if count <= 0 {
		_, total, err := h.adminService.ListUsers(c.Request.Context(), 1, 1, filters, "created_at", "desc")
		return nil, total, err
	}
	firstPage := offset/pageSize + 1
	withinPage := offset % pageSize
	users, total, err := h.adminService.ListUsers(c.Request.Context(), firstPage, pageSize, filters, "created_at", "desc")
	if err != nil {
		return nil, 0, err
	}
	result := make([]service.User, 0, count)
	if withinPage < len(users) {
		end := minIntValue(withinPage+count, len(users))
		result = append(result, users[withinPage:end]...)
	}
	if len(result) < count && int64(offset+len(result)) < total {
		next, _, nextErr := h.adminService.ListUsers(c.Request.Context(), firstPage+1, pageSize, filters, "created_at", "desc")
		if nextErr != nil {
			return nil, 0, nextErr
		}
		result = append(result, next[:minIntValue(count-len(result), len(next))]...)
	}
	return result, total, nil
}

func riskIndexRows(items []riskIndexListItem) []map[string]any {
	rows := make([]map[string]any, 0, len(items))
	for _, item := range items {
		rows = append(rows, map[string]any{
			"id": item.ID, "risk_type": item.RiskType, "risk_level": item.RiskLevel, "risk_score": item.Score, "risk_reason": item.Reason,
			"event_count": item.EventCount, "ip_count": item.IPCount, "device_count": item.DeviceCount, "last_action": item.LastAction,
			"pending": item.Pending, "last_event_at": item.LastEventAt, "last_risk_at": item.LastEventAt, "processing_status": item.ProcessingStatus,
			"case_id": item.CaseID, "case_status": item.CaseStatus, "assignee_id": item.AssigneeID, "evidence_strength": item.EvidenceStrength,
			"decision_id": item.DecisionID, "historical_max_score": item.HistoricalMaxScore,
		})
	}
	return rows
}

func riskMapUserIDs(items []map[string]any) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		if id, ok := item["id"].(int64); ok && id > 0 {
			ids = append(ids, id)
		}
	}
	return ids
}

func mergeRiskAccount(row map[string]any, userID int64, account map[string]any, lookupAvailable bool) {
	for key, value := range riskAccountRow(userID, account, lookupAvailable) {
		row[key] = value
	}
}

func minIntValue(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func maxIntValue(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func riskRowLastEvent(row map[string]any) string {
	value, _ := row["last_event_at"].(string)
	return strings.TrimSpace(value)
}

func (h *CustomUserHandler) riskIdentityAccounts(c *gin.Context, ids []int64) (map[int64]map[string]any, bool) {
	result := make(map[int64]map[string]any, len(ids))
	reader, ok := h.adminService.(service.RiskIdentityUserBatchReader)
	if !ok {
		return result, false
	}
	for start := 0; start < len(ids); start += 100 {
		end := start + 100
		if end > len(ids) {
			end = len(ids)
		}
		users, err := reader.GetUsersForRiskIdentity(c.Request.Context(), ids[start:end])
		if err != nil {
			return map[int64]map[string]any{}, false
		}
		for index := range users {
			result[users[index].ID] = identityAccountPayload(&users[index])
		}
	}
	return result, true
}

func (h *CustomUserHandler) attachRiskIdentitySummaries(c *gin.Context, items []map[string]any) {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		if id, ok := item["id"].(int64); ok && id > 0 {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return
	}
	query := url.Values{"user_ids": {joinRiskUserIDs(ids)}}
	body, status, err := h.proxyRiskIdentity(c, http.MethodGet, "/api/v1/admin/identity-summaries?"+query.Encode(), nil)
	if err != nil || status < 200 || status >= 300 {
		return
	}
	var summaries riskIdentitySummaryPage
	if json.Unmarshal(body, &summaries) != nil {
		return
	}
	byID := make(map[int64]map[string]any, len(summaries.Items))
	for _, summary := range summaries.Items {
		if raw, ok := summary["user_id"].(float64); ok {
			byID[int64(raw)] = summary
		}
	}
	for _, item := range items {
		if id, ok := item["id"].(int64); ok {
			item["identity"] = byID[id]
		}
	}
}

func riskAccountRow(userID int64, account map[string]any, lookupAvailable bool) map[string]any {
	if account == nil {
		availability := "unavailable"
		if lookupAvailable {
			availability = "deleted"
		}
		return map[string]any{"id": userID, "email": "", "username": "", "status": "", "account_availability": availability}
	}
	return map[string]any{"id": userID, "email": account["email"], "username": account["username"], "status": account["status"], "created_at": account["created_at"], "account_availability": account["availability"]}
}

func riskAccountMatches(account map[string]any, _ bool, search, status string) bool {
	if account == nil {
		return strings.TrimSpace(search) == "" && strings.TrimSpace(status) == ""
	}
	if wanted := strings.TrimSpace(status); wanted != "" && fmt.Sprint(account["status"]) != wanted {
		return false
	}
	needle := strings.ToLower(strings.TrimSpace(search))
	if needle == "" {
		return true
	}
	return strings.Contains(strings.ToLower(fmt.Sprint(account["email"])), needle) || strings.Contains(strings.ToLower(fmt.Sprint(account["username"])), needle) || strings.Contains(fmt.Sprint(account["id"]), needle)
}

func riskCaseUserIDs(items []riskCaseListItem) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.UserID)
	}
	return ids
}

func joinRiskUserIDs(ids []int64) string {
	values := make([]string, 0, len(ids))
	for _, id := range ids {
		values = append(values, strconv.FormatInt(id, 10))
	}
	return strings.Join(values, ",")
}

func riskLevelForScore(score int) string {
	switch {
	case score >= 80:
		return "critical"
	case score >= 60:
		return "high"
	case score >= 30:
		return "medium"
	case score > 0:
		return "low"
	default:
		return "none"
	}
}

func copyRiskQuery(target url.Values, c *gin.Context, keys ...string) {
	for _, key := range keys {
		if value := strings.TrimSpace(c.Query(key)); value != "" {
			target.Set(key, value)
		}
	}
}

func normalizedSortOrder(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "asc") {
		return "asc"
	}
	return "desc"
}
