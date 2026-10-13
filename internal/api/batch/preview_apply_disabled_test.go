//go:build precision_preview

package batch

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/stretchr/testify/require"
)

// These are actual handler tests; nil runtime deliberately proves that the
// preview check is performed *before* any job/cache/file interaction.
func TestPrecisionPreviewEndpointsRejectAllMutations(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		handler gin.HandlerFunc
	}{
		{"organize", "/batch/any/organize", organizeJob(nil)},
		{"update", "/batch/any/update", updateBatchJob(nil)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.POST(tt.path, tt.handler)
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, tt.path, nil)
			router.ServeHTTP(recorder, req)
			require.Equal(t, http.StatusConflict, recorder.Code, recorder.Body.String())
			var msg contracts.ErrorResponse
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &msg))
			require.Equal(t, "PREVIEW_APPLY_DISABLED", msg.Code)
		})
	}
}
