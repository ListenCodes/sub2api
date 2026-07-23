//go:build unit

package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type customReleaseHandlerServiceStub struct {
	*systemHandlerUpdateServiceStub
	customInfo  *service.CustomReleaseInfo
	job         *service.UpdateJob
	prepareCall int
	applyCall   int
	statusCall  int
}

func (s *customReleaseHandlerServiceStub) CheckCustomRelease(context.Context, bool) (*service.CustomReleaseInfo, error) {
	return s.customInfo, nil
}

func (s *customReleaseHandlerServiceStub) PrepareUpdate(context.Context) (*service.UpdateJob, error) {
	s.prepareCall++
	return s.job, nil
}

func (s *customReleaseHandlerServiceStub) ApplyUpdate(context.Context, string) (*service.UpdateJob, error) {
	s.applyCall++
	return s.job, nil
}

func (s *customReleaseHandlerServiceStub) GetUpdateStatus(context.Context, string) (*service.UpdateJob, error) {
	s.statusCall++
	return s.job, nil
}

func TestCustomReleaseLegacyRollbackFailsClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stableStub := &systemHandlerUpdateServiceStub{}
	stub := &customReleaseHandlerServiceStub{systemHandlerUpdateServiceStub: stableStub}
	handler := NewSystemHandler(stub, service.NewSystemOperationLockService(newMemoryIdempotencyRepoStub(), service.IdempotencyConfig{}))
	router := gin.New()
	router.POST("/api/v1/admin/system/rollback", handler.LegacyRollbackUnsupported)
	router.GET("/api/v1/admin/system/rollback-versions", handler.LegacyRollbackUnsupported)

	for _, tc := range []struct {
		method string
		path   string
	}{
		{method: http.MethodPost, path: "/api/v1/admin/system/rollback"},
		{method: http.MethodGet, path: "/api/v1/admin/system/rollback-versions"},
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(tc.method, tc.path, nil)
		router.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusConflict, recorder.Code)
	}

	require.Zero(t, stableStub.rollbackCall)
	require.Zero(t, stableStub.rollbackToCall)
	require.Zero(t, stableStub.rollbackVersionsCall)
}
