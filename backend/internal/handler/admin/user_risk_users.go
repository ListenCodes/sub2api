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
	LastHitAt          string `json:"last_hit_at"`
}

type riskCaseListPage struct {
	Items    []riskCaseListItem `json:"items"`
	Total    int                `json:"total"`
	Page     int                `json:"page"`
	PageSize int                `json:"page_size"`
}

type riskSubjectListPage struct {
	Items []struct {
		ID          int64  `json:"id"`
		RiskType    string `json:"risk_type"`
		RiskLevel   string `json:"risk_level"`
		Score       int    `json:"score"`
		Reason      string `json:"reason"`
		EventCount  int    `json:"event_count"`
		IPCount     int    `json:"ip_count"`
		DeviceCount int    `json:"device_count"`
		LastAction  string `json:"last_action"`
		Pending     bool   `json:"pending"`
		LastEventAt string `json:"last_event_at"`
	} `json:"items"`
}

type riskIdentitySummaryPage struct {
	Items []map[string]any `json:"items"`
}

// ListUserRiskUsers is the custom-owned aggregation boundary. It keeps account
// data in the main service while cases, current scores and evidence stay in the
// extension, and returns one already-paginated response to the browser.
func (h *CustomUserHandler) ListUserRiskUsers(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	if pageSize > 100 {
		pageSize = 100
	}
	view := strings.TrimSpace(c.DefaultQuery("view", "pending"))
	caseFiltered := hasRiskCaseFilters(c)
	if view == "all" && !caseFiltered {
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
	if strings.TrimSpace(c.Query("sort_by")) == "risk_score" {
		h.listAllUserRiskUsersSortedByRisk(c, page, pageSize, filters)
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
	if len(ids) > 0 && !h.attachRiskSubjects(c, items) {
		return
	}
	h.attachRiskIdentitySummaries(c, items)
	response.Paginated(c, items, total, page, pageSize)
}

func (h *CustomUserHandler) listAllUserRiskUsersSortedByRisk(c *gin.Context, page, pageSize int, filters service.UserListFilters) {
	const accountBatchSize = 1000
	var users []service.User
	var total int64
	for batchPage := 1; ; batchPage++ {
		batch, batchTotal, err := h.adminService.ListUsers(c.Request.Context(), batchPage, accountBatchSize, filters, "created_at", "desc")
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		if batchPage == 1 {
			total = batchTotal
		}
		users = append(users, batch...)
		if len(batch) == 0 || int64(len(users)) >= total {
			break
		}
	}
	items := make([]map[string]any, 0, len(users))
	for index := range users {
		items = append(items, riskAccountRow(users[index].ID, identityAccountPayload(&users[index]), true))
	}
	if len(items) > 0 && !h.attachRiskSubjects(c, items) {
		return
	}
	descending := normalizedSortOrder(c.Query("sort_order")) == "desc"
	sort.SliceStable(items, func(left, right int) bool {
		leftScore, rightScore := riskRowScore(items[left]), riskRowScore(items[right])
		if leftScore == rightScore {
			return items[left]["id"].(int64) < items[right]["id"].(int64)
		}
		if descending {
			return leftScore > rightScore
		}
		return leftScore < rightScore
	})
	start := (page - 1) * pageSize
	if start > len(items) {
		start = len(items)
	}
	end := start + pageSize
	if end > len(items) {
		end = len(items)
	}
	items = items[start:end]
	h.attachRiskIdentitySummaries(c, items)
	response.Paginated(c, items, total, page, pageSize)
}

func (h *CustomUserHandler) attachRiskSubjects(c *gin.Context, items []map[string]any) bool {
	byID := make(map[int64]map[string]any, len(items))
	ids := make([]int64, 0, len(items))
	for _, row := range items {
		id, ok := row["id"].(int64)
		if !ok || id <= 0 {
			continue
		}
		ids = append(ids, id)
		byID[id] = row
	}
	for start := 0; start < len(ids); start += 100 {
		end := start + 100
		if end > len(ids) {
			end = len(ids)
		}
		query := url.Values{"user_ids": {joinRiskUserIDs(ids[start:end])}, "limit": {strconv.Itoa(end - start)}}
		body, status, err := h.proxyRiskIdentity(c, http.MethodGet, "/api/v1/admin/users?"+query.Encode(), nil)
		if err != nil {
			return false
		}
		if status < 200 || status >= 300 {
			c.Data(status, "application/json", body)
			return false
		}
		var subjects riskSubjectListPage
		if json.Unmarshal(body, &subjects) != nil {
			response.Error(c, http.StatusBadGateway, "Risk subject response is invalid")
			return false
		}
		for _, subject := range subjects.Items {
			if row := byID[subject.ID]; row != nil {
				row["risk_type"], row["risk_level"], row["risk_score"], row["risk_reason"] = subject.RiskType, subject.RiskLevel, subject.Score, subject.Reason
				row["event_count"], row["ip_count"], row["device_count"] = subject.EventCount, subject.IPCount, subject.DeviceCount
				row["last_action"], row["pending"], row["last_event_at"] = subject.LastAction, subject.Pending, subject.LastEventAt
			}
		}
	}
	return true
}

func riskRowScore(row map[string]any) int {
	switch value := row["risk_score"].(type) {
	case int:
		return value
	case float64:
		return int(value)
	default:
		return 0
	}
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
