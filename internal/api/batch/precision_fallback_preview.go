package batch

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/precisionplan"
	"github.com/javinizer/javinizer-go/internal/worker"
)

// makeSupplementalPreview is a read-only compatibility shim for the upstream
// GitHub base branch. It NEVER promotes old "completed" results to confirmed.
// The Mac-local precision-first audit is newer and must supply confirmed
// verdicts only after the package is merged into that local tree.
func makeSupplementalPreview(status *worker.BatchJobStatus) (precisionplan.Plan, []precisionplan.QueryGroup, error) {
	if status == nil {
		return precisionplan.Plan{}, nil, nil
	}
	files := make([]precisionplan.File, 0, len(status.Results))
	for path, r := range status.Results {
		f := precisionplan.File{Path: path, Excluded: status.Excluded[path]}
		if r != nil {
			f.ParsedSKU = r.FileMatchInfo.MovieID
			f.CurrentStatus = string(r.Status)
			if r.Movie != nil {
				f.WorkID = r.Movie.ContentID
				f.OriginalTitle = r.Movie.OriginalTitle
			}
		}
		files = append(files, f)
	}
	plan, err := precisionplan.Build(files, nil) // intentionally NO trusted verdicts
	if err != nil {
		return precisionplan.Plan{}, nil, err
	}
	return plan, plan.Queries(), nil
}

// previewPrecisionFallback returns only a query plan. No rescrape, network
// search, cache write, file operation, or persist call occurs here.
func previewPrecisionFallback(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		status, ok := rt.Deps().GetJobStore().GetJob(c.Param("id"))
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
			return
		}
		plan, queries, err := makeSupplementalPreview(status)
		if err != nil {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "unable to build read-only preview"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"read_only":                             true,
			"requires_authoritative_identity_audit": true,
			"ready_count":                           0,
			"policy_version":                        precisionplan.PolicyVersion,
			"plan":                                  plan,
			"queries":                               queries,
		})
	}
}
