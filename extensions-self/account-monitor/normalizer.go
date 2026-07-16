package accountmonitor

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

type UpstreamError struct {
	AtUnixMS           int64  `json:"at_unix_ms,omitempty"`
	Platform           string `json:"platform,omitempty"`
	AccountID          int64  `json:"account_id,omitempty"`
	AccountName        string `json:"account_name,omitempty"`
	UpstreamModel      string `json:"upstream_model,omitempty"`
	UpstreamStatusCode int    `json:"upstream_status_code,omitempty"`
	Kind               string `json:"kind,omitempty"`
	Message            string `json:"message,omitempty"`
	Detail             string `json:"detail,omitempty"`
}

func Normalize(usageRows []UsageSourceRow, errorRows []ErrorSourceRow) (Batch, error) {
	batch := Batch{}
	requestIndexes := make(map[string]int)

	for _, row := range errorRows {
		requestKey, identity := requestKey("ops", row.ID, row.APIKeyID, row.RequestID)
		var events []UpstreamError
		if len(row.UpstreamErrors) > 0 && string(row.UpstreamErrors) != "null" {
			if err := json.Unmarshal(row.UpstreamErrors, &events); err != nil {
				return Batch{}, fmt.Errorf("decode upstream errors for ops log %d: %w", row.ID, err)
			}
		}
		if len(events) > 0 {
			for index, event := range events {
				if event.AccountID <= 0 {
					continue
				}
				model, attribution := actualModel(event.UpstreamModel, row.UpstreamModel, row.RequestedModel, row.Model)
				attemptedAt := row.CreatedAt
				if event.AtUnixMS > 0 {
					attemptedAt = time.UnixMilli(event.AtUnixMS).UTC()
				}
				platform := strings.TrimSpace(event.Platform)
				if platform == "" {
					platform = strings.TrimSpace(row.Platform)
				}
				category := ClassifyFailure(FailureSignal{
					ProviderCode: row.ProviderErrorCode, ProviderType: row.ProviderErrorType,
					UpstreamStatus: event.UpstreamStatusCode, StatusCode: row.StatusCode,
					ErrorType: row.ErrorType, ErrorPhase: row.ErrorPhase,
					NetworkType: row.NetworkErrorType, Message: event.Message + " " + event.Detail,
				})
				batch.Attempts = append(batch.Attempts, AttemptFact{
					EventKey:           fmt.Sprintf("ops:%d:event:%d", row.ID, index),
					RequestKey:         requestKey,
					AttemptedAt:        attemptedAt,
					AccountID:          event.AccountID,
					Platform:           platform,
					ActualModel:        model,
					ModelAttribution:   attribution,
					UserID:             row.UserID,
					APIKeyID:           row.APIKeyID,
					RequestType:        row.RequestType,
					Result:             ResultFailed,
					Recovered:          row.StatusCode > 0 && row.StatusCode < 400,
					ErrorCategory:      category,
					StatusCode:         row.StatusCode,
					UpstreamStatusCode: event.UpstreamStatusCode,
					ProviderErrorCode:  row.ProviderErrorCode,
					DurationMS:         row.DurationMS,
					IdentityQuality:    identity,
					SourceKind:         "ops",
					SourceID:           row.ID,
				})
			}
		} else if row.AccountID > 0 && isUpstreamFailure(row) {
			model, attribution := actualModel("", row.UpstreamModel, row.RequestedModel, row.Model)
			category := classifyErrorRow(row)
			batch.Attempts = append(batch.Attempts, AttemptFact{
				EventKey:           fmt.Sprintf("ops:%d:synthetic", row.ID),
				RequestKey:         requestKey,
				AttemptedAt:        row.CreatedAt,
				AccountID:          row.AccountID,
				Platform:           row.Platform,
				ActualModel:        model,
				ModelAttribution:   attribution,
				UserID:             row.UserID,
				APIKeyID:           row.APIKeyID,
				RequestType:        row.RequestType,
				Result:             ResultFailed,
				Recovered:          row.StatusCode > 0 && row.StatusCode < 400,
				ErrorCategory:      category,
				StatusCode:         row.StatusCode,
				UpstreamStatusCode: row.UpstreamStatusCode,
				ProviderErrorCode:  row.ProviderErrorCode,
				DurationMS:         row.DurationMS,
				IdentityQuality:    identity,
				SourceKind:         "ops",
				SourceID:           row.ID,
			})
		}
		if row.StatusCode >= 400 {
			model, attribution := actualModel("", row.UpstreamModel, row.RequestedModel, row.Model)
			category := classifyErrorRow(row)
			upsertRequest(&batch.Requests, requestIndexes, RequestFact{
				RequestKey:       requestKey,
				OccurredAt:       row.CreatedAt,
				UserID:           row.UserID,
				APIKeyID:         row.APIKeyID,
				AccountID:        row.AccountID,
				GroupID:          row.GroupID,
				Platform:         row.Platform,
				ActualModel:      model,
				ModelAttribution: attribution,
				RequestType:      row.RequestType,
				Result:           ResultFailed,
				ErrorCategory:    category,
				StatusCode:       row.StatusCode,
				DurationMS:       row.DurationMS,
				IdentityQuality:  identity,
				SourceKind:       "ops",
				SourceID:         row.ID,
			})
		}
		batch.ErrorCursor = laterCursor(batch.ErrorCursor, Cursor{Time: row.CreatedAt, ID: row.ID})
	}

	for _, row := range usageRows {
		batch.UsageCursor = laterCursor(batch.UsageCursor, Cursor{Time: row.CreatedAt, ID: row.ID})
		if row.ActualCost <= 0 {
			continue
		}
		requestKey, identity := requestKey("usage", row.ID, row.APIKeyID, row.RequestID)
		model, attribution := actualModel(row.UpstreamModel, "", row.RequestedModel, row.Model)
		multiplier := 1.0
		if row.AccountMultiplierSet {
			multiplier = row.AccountRateMultiplier
		} else if row.AccountRateMultiplier != 0 {
			multiplier = row.AccountRateMultiplier
		}
		accountCost := math.Round(row.TotalCost*multiplier*1e10) / 1e10
		attempt := AttemptFact{
			EventKey:             fmt.Sprintf("usage:%d", row.ID),
			RequestKey:           requestKey,
			AttemptedAt:          row.CreatedAt,
			AccountID:            row.AccountID,
			ParentAccountID:      row.ParentAccountID,
			Platform:             row.Platform,
			ActualModel:          model,
			ModelAttribution:     attribution,
			UserID:               row.UserID,
			APIKeyID:             row.APIKeyID,
			RequestType:          row.RequestType,
			Result:               ResultSucceeded,
			InputTokens:          row.InputTokens,
			OutputTokens:         row.OutputTokens,
			CacheCreationTokens:  row.CacheCreationTokens,
			CacheReadTokens:      row.CacheReadTokens,
			UserCost:             row.ActualCost,
			AccountCost:          accountCost,
			DurationMS:           row.DurationMS,
			ImageCount:           row.ImageCount,
			ImageSize:            row.ImageSize,
			VideoCount:           row.VideoCount,
			VideoResolution:      row.VideoResolution,
			VideoDurationSeconds: row.VideoDurationSeconds,
			IdentityQuality:      identity,
			SourceKind:           "usage",
			SourceID:             row.ID,
		}
		batch.Attempts = append(batch.Attempts, attempt)
		upsertRequest(&batch.Requests, requestIndexes, RequestFact{
			RequestKey:           requestKey,
			OccurredAt:           row.CreatedAt,
			UserID:               row.UserID,
			APIKeyID:             row.APIKeyID,
			AccountID:            row.AccountID,
			GroupID:              row.GroupID,
			Platform:             row.Platform,
			ActualModel:          model,
			ModelAttribution:     attribution,
			RequestType:          row.RequestType,
			Result:               ResultSucceeded,
			InputTokens:          row.InputTokens,
			OutputTokens:         row.OutputTokens,
			CacheCreationTokens:  row.CacheCreationTokens,
			CacheReadTokens:      row.CacheReadTokens,
			UserCost:             row.ActualCost,
			AccountCost:          accountCost,
			DurationMS:           row.DurationMS,
			ImageCount:           row.ImageCount,
			VideoCount:           row.VideoCount,
			VideoResolution:      row.VideoResolution,
			VideoDurationSeconds: row.VideoDurationSeconds,
			IdentityQuality:      identity,
			SourceKind:           "usage",
			SourceID:             row.ID,
		})
	}

	return batch, nil
}

func requestKey(source string, sourceID, apiKeyID int64, requestID string) (string, IdentityQuality) {
	if requestID = strings.TrimSpace(requestID); requestID != "" {
		return fmt.Sprintf("request:%d:%s", apiKeyID, requestID), IdentityExact
	}
	return fmt.Sprintf("%s:%d", source, sourceID), IdentityFallback
}

func actualModel(exact string, fallbacks ...string) (string, AttributionQuality) {
	if exact = strings.TrimSpace(exact); exact != "" {
		return exact, AttributionExact
	}
	for _, candidate := range fallbacks {
		if candidate = strings.TrimSpace(candidate); candidate != "" {
			return candidate, AttributionEstimated
		}
	}
	return "", AttributionEstimated
}

func isUpstreamFailure(row ErrorSourceRow) bool {
	return row.UpstreamStatusCode > 0 || strings.EqualFold(row.ErrorPhase, "upstream") || strings.EqualFold(row.ErrorOwner, "upstream")
}

func classifyErrorRow(row ErrorSourceRow) ErrorCategory {
	return ClassifyFailure(FailureSignal{
		ProviderCode: row.ProviderErrorCode, ProviderType: row.ProviderErrorType,
		UpstreamStatus: row.UpstreamStatusCode, StatusCode: row.StatusCode,
		ErrorType: row.ErrorType, ErrorPhase: row.ErrorPhase,
		NetworkType: row.NetworkErrorType,
		Message:     row.ErrorMessage + " " + row.UpstreamErrorMessage,
	})
}

func upsertRequest(items *[]RequestFact, indexes map[string]int, fact RequestFact) {
	if index, ok := indexes[fact.RequestKey]; ok {
		existing := (*items)[index]
		if shouldReplaceRequest(existing, fact) {
			(*items)[index] = fact
		}
		return
	}
	indexes[fact.RequestKey] = len(*items)
	*items = append(*items, fact)
}

func shouldReplaceRequest(existing, candidate RequestFact) bool {
	if existing.Result == ResultSucceeded && candidate.Result != ResultSucceeded {
		return false
	}
	if existing.Result != ResultSucceeded && candidate.Result == ResultSucceeded {
		return true
	}
	return candidate.OccurredAt.After(existing.OccurredAt) ||
		(candidate.OccurredAt.Equal(existing.OccurredAt) && candidate.SourceID > existing.SourceID)
}

func laterCursor(current, candidate Cursor) Cursor {
	if candidate.Time.After(current.Time) || (candidate.Time.Equal(current.Time) && candidate.ID > current.ID) {
		return candidate
	}
	return current
}
