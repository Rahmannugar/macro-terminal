package adminjobs

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/adminjobs/models"
	"github.com/Rahmannugar/macro-terminal/server/internal/openapi"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type fakeFailedJobs struct {
	list        []models.Job
	next        *models.ListCursor
	job         models.Job
	replayed    models.Job
	listErr     error
	getErr      error
	replayErr   error
	gotType     *string
	gotLimit    int32
	gotCursor   *models.ListCursor
	replayCalls int
}

func (fake *fakeFailedJobs) FailedJobs(_ context.Context, jobType *string, cursor *models.ListCursor, limit int32) ([]models.Job, *models.ListCursor, error) {
	fake.gotType = jobType
	fake.gotLimit = limit
	fake.gotCursor = cursor
	return fake.list, fake.next, fake.listErr
}

func (fake *fakeFailedJobs) JobByID(_ context.Context, id uuid.UUID) (models.Job, error) {
	if fake.getErr != nil {
		return models.Job{}, fake.getErr
	}
	return fake.job, nil
}

func (fake *fakeFailedJobs) ReplayJob(_ context.Context, id uuid.UUID) (models.Job, error) {
	fake.replayCalls++
	if fake.replayErr != nil {
		return models.Job{}, fake.replayErr
	}
	return fake.replayed, nil
}

func adminJobsRouter(t *testing.T, fake *fakeFailedJobs) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterRoutes(router.Group("/api/v1/admin"), fake)
	return router
}

func perform(router *gin.Engine, method, target string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, strings.NewReader(""))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func decodeError(t *testing.T, recorder *httptest.ResponseRecorder) openapi.Error {
	t.Helper()
	var body openapi.Error
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode failure: %v", err)
	}
	return body
}

func TestListFailedJobsAppliesPagingAndTypeFilter(t *testing.T) {
	fake := &fakeFailedJobs{
		list: []models.Job{{ID: uuid.New(), Type: "article_enrichment", Status: models.StatusFailed}},
	}
	router := adminJobsRouter(t, fake)

	recorder := perform(router, http.MethodGet, "/api/v1/admin/jobs/failed?type=article_enrichment&limit=10")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body jobListResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Jobs) != 1 || body.NextCursor != nil {
		t.Errorf("body = %+v, want one job and a null nextCursor on the last page", body)
	}
	if fake.gotType == nil || *fake.gotType != "article_enrichment" || fake.gotLimit != 10 || fake.gotCursor != nil {
		t.Errorf("filter/limit/cursor = %v/%d/%v, want article_enrichment/10/none", fake.gotType, fake.gotLimit, fake.gotCursor)
	}
}

func TestListFailedJobsRejectsBadPageParameters(t *testing.T) {
	router := adminJobsRouter(t, &fakeFailedJobs{})

	recorder := perform(router, http.MethodGet, "/api/v1/admin/jobs/failed?limit=abc")
	if recorder.Code != http.StatusBadRequest || decodeError(t, recorder).Error.Code != "invalid_limit" {
		t.Errorf("status/code = %d/%q, want 400 invalid_limit", recorder.Code, decodeError(t, recorder).Error.Code)
	}

	recorder = perform(router, http.MethodGet, "/api/v1/admin/jobs/failed?cursor=not-base64!!")
	if recorder.Code != http.StatusBadRequest || decodeError(t, recorder).Error.Code != "invalid_cursor" {
		t.Errorf("status/code = %d/%q, want 400 invalid_cursor", recorder.Code, decodeError(t, recorder).Error.Code)
	}
}

func TestReplayJobValidatesIdAndFindsJob(t *testing.T) {
	router := adminJobsRouter(t, &fakeFailedJobs{})

	recorder := perform(router, http.MethodPost, "/api/v1/admin/jobs/whenever/replay")
	if recorder.Code != http.StatusBadRequest || decodeError(t, recorder).Error.Code != "invalid_id" {
		t.Errorf("status/code = %d/%q, want 400 invalid_id", recorder.Code, decodeError(t, recorder).Error.Code)
	}

	missing := adminJobsRouter(t, &fakeFailedJobs{getErr: pgx.ErrNoRows})
	recorder = perform(missing, http.MethodPost, "/api/v1/admin/jobs/"+uuid.NewString()+"/replay")
	if recorder.Code != http.StatusNotFound || decodeError(t, recorder).Error.Code != "not_found" {
		t.Errorf("status/code = %d/%q, want 404 not_found", recorder.Code, decodeError(t, recorder).Error.Code)
	}
}

func TestReplayReturnsFailedJobToPending(t *testing.T) {
	id := uuid.New()
	lastError := "provider unavailable"
	fake := &fakeFailedJobs{
		job:      models.Job{ID: id, Type: "asset_notification", Status: models.StatusFailed, Attempts: 8, LastError: &lastError, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		replayed: models.Job{ID: id, Type: "asset_notification", Status: models.StatusPending, Attempts: 0},
	}
	router := adminJobsRouter(t, fake)

	recorder := perform(router, http.MethodPost, "/api/v1/admin/jobs/"+id.String()+"/replay")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body jobResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Job.Status != models.StatusPending || body.Job.Attempts != 0 {
		t.Errorf("job = %+v, want pending with zero attempts", body.Job)
	}
	if fake.replayCalls != 1 {
		t.Errorf("replay calls = %d, want 1", fake.replayCalls)
	}
}

func TestReplayIsIdempotentForPendingJobs(t *testing.T) {
	id := uuid.New()
	fake := &fakeFailedJobs{job: models.Job{ID: id, Status: models.StatusPending}}
	router := adminJobsRouter(t, fake)

	recorder := perform(router, http.MethodPost, "/api/v1/admin/jobs/"+id.String()+"/replay")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if fake.replayCalls != 0 {
		t.Errorf("replay calls = %d, want 0 for a pending job", fake.replayCalls)
	}
}

func TestReplayRefusesJobsOutsideTheRetryPath(t *testing.T) {
	id := uuid.New()
	fake := &fakeFailedJobs{job: models.Job{ID: id, Status: models.StatusDone}}
	router := adminJobsRouter(t, fake)

	recorder := perform(router, http.MethodPost, "/api/v1/admin/jobs/"+id.String()+"/replay")
	if recorder.Code != http.StatusConflict || decodeError(t, recorder).Error.Code != "job_not_replayable" {
		t.Errorf("status/code = %d/%q, want 409 job_not_replayable", recorder.Code, decodeError(t, recorder).Error.Code)
	}
	if fake.replayCalls != 0 {
		t.Errorf("replay calls = %d, want 0", fake.replayCalls)
	}
}

func TestReplayReportsConflictWhenJobMovesOn(t *testing.T) {
	id := uuid.New()
	fake := &fakeFailedJobs{
		job:       models.Job{ID: id, Status: models.StatusFailed},
		replayErr: pgx.ErrNoRows,
	}
	router := adminJobsRouter(t, fake)

	recorder := perform(router, http.MethodPost, "/api/v1/admin/jobs/"+id.String()+"/replay")
	if recorder.Code != http.StatusConflict || decodeError(t, recorder).Error.Code != "job_not_replayable" {
		t.Errorf("status/code = %d/%q, want 409 job_not_replayable", recorder.Code, decodeError(t, recorder).Error.Code)
	}
}

func TestListFailedJobsReportsRepositoryErrors(t *testing.T) {
	fake := &fakeFailedJobs{listErr: errors.New("database down")}
	router := adminJobsRouter(t, fake)

	recorder := perform(router, http.MethodGet, "/api/v1/admin/jobs/failed")
	if recorder.Code != http.StatusInternalServerError || decodeError(t, recorder).Error.Code != "internal_error" {
		t.Errorf("status/code = %d/%q, want 500 internal_error", recorder.Code, decodeError(t, recorder).Error.Code)
	}
}
