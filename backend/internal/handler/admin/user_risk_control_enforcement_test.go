package admin

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestManualStatusChangeRevokesTokensOnlyOnDisableTransition(t *testing.T) {
	if !shouldRevokeTokensForStatusChange(service.StatusActive, service.StatusDisabled) {
		t.Fatal("active to disabled must revoke tokens")
	}
	if shouldRevokeTokensForStatusChange(service.StatusDisabled, service.StatusDisabled) {
		t.Fatal("disabled to disabled must not revoke tokens again")
	}
	if shouldRevokeTokensForStatusChange(service.StatusDisabled, service.StatusActive) {
		t.Fatal("unban must not revoke tokens")
	}
}

func TestRiskBanRevokesTokensEvenWhenUserAlreadyDisabled(t *testing.T) {
	user := &service.User{Status: service.StatusDisabled}
	if !shouldRevokeTokensForRiskBan(user) {
		t.Fatal("automatic ban must revoke tokens even for an already disabled user")
	}
}
