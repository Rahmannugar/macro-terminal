package adminjobs

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/adminjobs/models"
	"github.com/Rahmannugar/macro-terminal/server/internal/common/paging"
	"github.com/Rahmannugar/macro-terminal/server/internal/openapi"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	defaultPageSize int32 = 25
	maximumPageSize int32 = 100
)

type FailedJobs interface {
	FailedJobs(ctx context.Context, jobType *string, cursor *paging.Cursor, limit int32) ([]models.Job, *paging.Cursor, error)
	JobByID(ctx context.Context, id uuid.UUID) (models.Job, error)
	ReplayJob(ctx context.Context, id uuid.UUID) (models.Job, error)
}

func RegisterRoutes(router gin.IRoutes, jobs FailedJobs) {
	router.GET("/jobs/failed", listFailedJobs(jobs))
	router.POST("/jobs/:id/replay", replayJob(jobs))
}

type jobJSON struct {
	ID        uuid.UUID `json:"id" example:"3b033aa5-a1a9-404a-aac7-096b5361b7be"`
	Type      string    `json:"type" example:"article_enrichment"`
	Status    string    `json:"status" example:"failed"`
	Attempts  int32     `json:"attempts" example:"8"`
	LastError *string   `json:"lastError" example:"provider returned 503"`
	CreatedAt time.Time `json:"createdAt" example:"2026-10-06T09:00:00Z"`
	UpdatedAt time.Time `json:"updatedAt" example:"2026-10-06T09:30:00Z"`
}

type jobListResponse struct {
	Jobs       []jobJSON `json:"jobs"`
	NextCursor *string   `json:"nextCursor"`
}

type jobResponse struct {
	Job jobJSON `json:"job"`
}

// @Summary List failed jobs.
// @Description Returns durable work that exhausted its retries, newest first, optionally filtered by job type. These rows are the dead-letter queue: replay a job to return it to normal processing. Pass the returned nextCursor to fetch the next page.
// @Tags admin
// @Param type query string false "Filter by job type, for example article_enrichment or email_delivery."
// @Param limit query int false "Page size between 1 and 100. Defaults to 25."
// @Param cursor query string false "Opaque cursor returned as nextCursor by the previous page."
// @Success 200 {object} jobListResponse "The failed jobs and the cursor for the next page, if any."
// @Failure 400 {object} openapi.Error "A limit or cursor is invalid."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/admin/jobs/failed [get]
func listFailedJobs(jobs FailedJobs) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		limit, ok := parseLimit(ctx.Query("limit"))
		if !ok {
			writeFailure(ctx, http.StatusBadRequest, "invalid_limit", "Limit must be a number between 1 and 100.")
			return
		}
		cursor, err := paging.DecodeCursor(ctx.Query("cursor"))
		if err != nil {
			writeFailure(ctx, http.StatusBadRequest, "invalid_cursor", "Use the next cursor returned by the previous page.")
			return
		}
		var jobType *string
		if value := ctx.Query("type"); value != "" {
			jobType = &value
		}

		list, next, err := jobs.FailedJobs(ctx.Request.Context(), jobType, cursor, limit)
		if err != nil {
			slog.Default().ErrorContext(ctx.Request.Context(), "failed job list failed", "error", err)
			writeFailure(ctx, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
			return
		}
		ctx.JSON(http.StatusOK, newJobListResponse(list, next))
	}
}

// @Summary Replay a failed job.
// @Description Returns a dead-lettered job to pending state with its attempt counter reset, so workers pick it up again under normal retry rules. Replaying an already pending job is a no-op; jobs in any other state are refused.
// @Tags admin
// @Param id path string true "Job id (UUID)."
// @Success 200 {object} jobResponse "The job, now pending."
// @Failure 400 {object} openapi.Error "The id is not a valid UUID."
// @Failure 404 {object} openapi.Error "No job has that id."
// @Failure 409 {object} openapi.Error "The job is not in a replayable state."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/admin/jobs/{id}/replay [post]
func replayJob(jobs FailedJobs) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		id, err := uuid.Parse(ctx.Param("id"))
		if err != nil {
			writeFailure(ctx, http.StatusBadRequest, "invalid_id", "The id must be a UUID.")
			return
		}
		job, err := jobs.JobByID(ctx.Request.Context(), id)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			writeFailure(ctx, http.StatusNotFound, "not_found", "No job has that id.")
			return
		case err != nil:
			slog.Default().ErrorContext(ctx.Request.Context(), "job lookup failed", "error", err)
			writeFailure(ctx, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
			return
		}

		switch job.Status {
		case models.StatusFailed:
		case models.StatusPending:
			ctx.JSON(http.StatusOK, jobResponse{Job: newJobJSON(job)})
			return
		default:
			writeFailure(ctx, http.StatusConflict, "job_not_replayable", "Only failed jobs can be replayed.")
			return
		}

		replayed, err := jobs.ReplayJob(ctx.Request.Context(), id)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			writeFailure(ctx, http.StatusConflict, "job_not_replayable", "Only failed jobs can be replayed.")
		case err != nil:
			slog.Default().ErrorContext(ctx.Request.Context(), "job replay failed", "error", err)
			writeFailure(ctx, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		default:
			ctx.JSON(http.StatusOK, jobResponse{Job: newJobJSON(replayed)})
		}
	}
}

func newJobJSON(job models.Job) jobJSON {
	return jobJSON{
		ID:        job.ID,
		Type:      job.Type,
		Status:    job.Status,
		Attempts:  job.Attempts,
		LastError: job.LastError,
		CreatedAt: job.CreatedAt,
		UpdatedAt: job.UpdatedAt,
	}
}

func newJobListResponse(list []models.Job, next *paging.Cursor) jobListResponse {
	jobs := make([]jobJSON, 0, len(list))
	for _, job := range list {
		jobs = append(jobs, newJobJSON(job))
	}
	response := jobListResponse{Jobs: jobs}
	if next != nil {
		encoded := paging.EncodeCursor(*next)
		response.NextCursor = &encoded
	}
	return response
}

func parseLimit(value string) (int32, bool) {
	if value == "" {
		return defaultPageSize, true
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 1 || limit > int(maximumPageSize) {
		return 0, false
	}
	return int32(limit), true
}

func writeFailure(ctx *gin.Context, status int, code, message string) {
	ctx.JSON(status, openapi.Error{Error: openapi.ErrorDetail{Code: code, Message: message}})
}
