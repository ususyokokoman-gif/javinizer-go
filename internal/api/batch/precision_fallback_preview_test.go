package batch

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

func TestPrecisionFallbackPreviewHTTPReadOnly(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")
	paths := []string{"/tmp/SIRO-5622.mp4", "/tmp/DCV-093.mp4", "/tmp/English.mp4", "/tmp/123456.mp4"}
	job := deps.JobStore.CreateJobBatch(paths)
	setJobResult(job, paths[0], &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: paths[0], MovieID: "SIRO-5622"},
		Status:        models.JobStatusFailed,
	})
	setJobResult(job, paths[1], &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: paths[1], MovieID: "DCV-093"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "DCV-093", ContentID: "dcv093", OriginalTitle: "実際の日本語原題"},
	})
	setJobResult(job, paths[2], &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: paths[2], MovieID: "MIUM-1423"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "MIUM-1423", ContentID: "mium1423", OriginalTitle: "Only English"},
	})
	setJobResult(job, paths[3], &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: paths[3], MovieID: "123456"},
		Status:        models.JobStatusFailed,
	})
	before, err := json.Marshal(job.GetStatus())
	require.NoError(t, err)
	router := gin.New()
	router.GET("/batch/:id/precision-fallback-preview", previewPrecisionFallback(testkit.GetTestRuntime(deps)))
	req := httptest.NewRequest(http.MethodGet, "/batch/"+job.GetID()+"/precision-fallback-preview", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var got struct {
		ReadOnly      bool             `json:"read_only"`
		RequiresAudit bool             `json:"requires_authoritative_identity_audit"`
		ReadyCount    int              `json:"ready_count"`
		Queries       []QueryGroupJSON `json:"queries"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.True(t, got.ReadOnly)
	require.True(t, got.RequiresAudit)
	require.Zero(t, got.ReadyCount)
	require.Len(t, got.Queries, 2)
	after, err := json.Marshal(job.GetStatus())
	require.NoError(t, err)
	require.True(t, bytes.Equal(before, after), "preview mutated job state")
}

type QueryGroupJSON struct {
	SKU      string
	ReadOnly bool
}

func TestPrecisionFallbackPreview404(t *testing.T) {
	deps := createTestDeps(t, &config.Config{}, "")
	router := gin.New()
	router.GET("/batch/:id/precision-fallback-preview", previewPrecisionFallback(testkit.GetTestRuntime(deps)))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/batch/missing/precision-fallback-preview", nil))
	require.Equal(t, http.StatusNotFound, w.Code)
}
