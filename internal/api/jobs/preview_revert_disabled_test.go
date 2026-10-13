//go:build precision_preview

package jobs

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/stretchr/testify/require"
)

// Nil dependencies prove preview rejects before touching history or disk.
func TestPreviewRevertEndpointsAlwaysBlock(t *testing.T) {
	cases := []struct {
		path    string
		handler gin.HandlerFunc
	}{
		{"/jobs/any/revert", revertBatch(JobDeps{})},
		{"/jobs/any/operations/MODEL-123/revert", revertOperation(JobDeps{})},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			router := gin.New()
			router.POST(tc.path, tc.handler)
			req := httptest.NewRequest(http.MethodPost, tc.path, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
			var response contracts.ErrorResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
			require.Equal(t, "PREVIEW_REVERT_DISABLED", response.Code)
		})
	}
}
