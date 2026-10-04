package notification

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/notification/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type failureRecord struct {
	id          uuid.UUID
	maxAttempts int32
	cause       string
}

type fakeRepository struct {
	claimed       []models.ClaimedJob
	claimCalls    int
	subjects      map[uuid.UUID]models.Subject
	subjectErr    error
	fanOutArticle int64
	fanOutEvent   int64
	fanOutCalls   int
	fanOutErr     error
	completed     []uuid.UUID
	failures      []failureRecord
	permanents    []failureRecord
}

func (repository *fakeRepository) EnqueueMissingArticleNotifications(context.Context, int32) (int64, error) {
	return 0, nil
}

func (repository *fakeRepository) EnqueueMissingCalendarEventNotifications(context.Context, int32) (int64, error) {
	return 0, nil
}

func (repository *fakeRepository) ReclaimStaleNotificationJobs(context.Context) (int64, error) {
	return 0, nil
}

func (repository *fakeRepository) ClaimBatch(context.Context, int32) ([]models.ClaimedJob, error) {
	repository.claimCalls++
	return repository.claimed, nil
}

func (repository *fakeRepository) Complete(_ context.Context, id uuid.UUID) error {
	repository.completed = append(repository.completed, id)
	return nil
}

func (repository *fakeRepository) Fail(_ context.Context, id uuid.UUID, maxAttempts int32, cause string) error {
	repository.failures = append(repository.failures, failureRecord{id: id, maxAttempts: maxAttempts, cause: cause})
	return nil
}

func (repository *fakeRepository) FailPermanently(_ context.Context, id uuid.UUID, cause string) error {
	repository.permanents = append(repository.permanents, failureRecord{id: id, cause: cause})
	return nil
}

func (repository *fakeRepository) subject(id uuid.UUID) (models.Subject, error) {
	if repository.subjectErr != nil {
		return models.Subject{}, repository.subjectErr
	}
	subject, ok := repository.subjects[id]
	if !ok {
		return models.Subject{}, fmt.Errorf("subject %s: %w", id, pgx.ErrNoRows)
	}
	return subject, nil
}

func (repository *fakeRepository) ArticleSubject(_ context.Context, id uuid.UUID) (models.Subject, error) {
	return repository.subject(id)
}

func (repository *fakeRepository) CalendarEventSubject(_ context.Context, id uuid.UUID) (models.Subject, error) {
	return repository.subject(id)
}

func (repository *fakeRepository) FanOutArticle(_ context.Context, articleID uuid.UUID, title string) (int64, error) {
	repository.fanOutCalls++
	if repository.fanOutErr != nil {
		return 0, repository.fanOutErr
	}
	return repository.fanOutArticle, nil
}

func (repository *fakeRepository) FanOutCalendarEvent(_ context.Context, eventID uuid.UUID, title string) (int64, error) {
	repository.fanOutCalls++
	if repository.fanOutErr != nil {
		return 0, repository.fanOutErr
	}
	return repository.fanOutEvent, nil
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
}

func TestCycleFansOutClaimedArticle(t *testing.T) {
	workID := uuid.New()
	articleID := uuid.New()
	repository := &fakeRepository{
		claimed: []models.ClaimedJob{{ID: workID, SubjectType: "article", SubjectID: articleID, Attempts: 1}},
		subjects: map[uuid.UUID]models.Subject{
			articleID: {ID: articleID, Title: "Fed holds rates steady"},
		},
		fanOutArticle: 2,
	}

	if err := NewJob(repository, testLogger()).cycle(t.Context()); err != nil {
		t.Fatalf("cycle: %v", err)
	}

	if repository.fanOutCalls != 1 {
		t.Fatalf("fan-out calls = %d, want 1", repository.fanOutCalls)
	}
	if len(repository.completed) != 1 || repository.completed[0] != workID {
		t.Errorf("completed = %v, want [%v]", repository.completed, workID)
	}
	if len(repository.failures) != 0 || len(repository.permanents) != 0 {
		t.Errorf("failures = %v permanents = %v, want none", repository.failures, repository.permanents)
	}
}

func TestCycleFansOutClaimedCalendarEvent(t *testing.T) {
	workID := uuid.New()
	eventID := uuid.New()
	repository := &fakeRepository{
		claimed: []models.ClaimedJob{{ID: workID, SubjectType: "calendar_event", SubjectID: eventID, Attempts: 1}},
		subjects: map[uuid.UUID]models.Subject{
			eventID: {ID: eventID, Title: "CPI"},
		},
		fanOutEvent: 1,
	}

	if err := NewJob(repository, testLogger()).cycle(t.Context()); err != nil {
		t.Fatalf("cycle: %v", err)
	}

	if repository.fanOutCalls != 1 {
		t.Fatalf("fan-out calls = %d, want 1", repository.fanOutCalls)
	}
	if len(repository.completed) != 1 || repository.completed[0] != workID {
		t.Errorf("completed = %v, want [%v]", repository.completed, workID)
	}
}

func TestCyclePermanentlyFailsMissingSubject(t *testing.T) {
	workID := uuid.New()
	articleID := uuid.New()
	repository := &fakeRepository{
		claimed:    []models.ClaimedJob{{ID: workID, SubjectType: "article", SubjectID: articleID, Attempts: 1}},
		subjectErr: fmt.Errorf("article %s: %w", articleID, pgx.ErrNoRows),
	}

	if err := NewJob(repository, testLogger()).cycle(t.Context()); err != nil {
		t.Fatalf("cycle: %v", err)
	}

	if repository.fanOutCalls != 0 {
		t.Errorf("fan-out calls = %d, want 0", repository.fanOutCalls)
	}
	if len(repository.permanents) != 1 || repository.permanents[0].id != workID {
		t.Errorf("permanents = %v, want the claimed row", repository.permanents)
	}
	if repository.permanents[0].cause != "article not found" {
		t.Errorf("cause = %q, want the missing-subject cause", repository.permanents[0].cause)
	}
	if len(repository.failures) != 0 {
		t.Errorf("failures = %v, want none", repository.failures)
	}
}

func TestCycleSendsFanOutFailureBackWithBackoff(t *testing.T) {
	workID := uuid.New()
	eventID := uuid.New()
	repository := &fakeRepository{
		claimed: []models.ClaimedJob{{ID: workID, SubjectType: "calendar_event", SubjectID: eventID, Attempts: 3}},
		subjects: map[uuid.UUID]models.Subject{
			eventID: {ID: eventID, Title: "NFP"},
		},
		fanOutErr: errors.New("connection refused"),
	}

	if err := NewJob(repository, testLogger()).cycle(t.Context()); err == nil {
		t.Fatal("cycle succeeded, want the fan-out error reported")
	}

	if len(repository.failures) != 1 {
		t.Fatalf("failures = %v, want one backoff failure", repository.failures)
	}
	failure := repository.failures[0]
	if failure.id != workID || failure.maxAttempts != maxAttempts {
		t.Errorf("failure = %+v, want the claimed row and the attempt budget", failure)
	}
	if failure.cause != "connection refused" {
		t.Errorf("cause = %q, want the fan-out error", failure.cause)
	}
	if len(repository.completed) != 0 || len(repository.permanents) != 0 {
		t.Errorf("completed = %v permanents = %v, want none", repository.completed, repository.permanents)
	}
}

func TestCycleFailsUnknownSubjectTypePermanently(t *testing.T) {
	workID := uuid.New()
	repository := &fakeRepository{
		claimed: []models.ClaimedJob{{ID: workID, SubjectType: "price_alert", SubjectID: uuid.New(), Attempts: 1}},
	}

	if err := NewJob(repository, testLogger()).cycle(t.Context()); err != nil {
		t.Fatalf("cycle: %v", err)
	}

	if repository.fanOutCalls != 0 {
		t.Errorf("fan-out calls = %d, want 0", repository.fanOutCalls)
	}
	if len(repository.permanents) != 1 || repository.permanents[0].id != workID {
		t.Errorf("permanents = %v, want the claimed row", repository.permanents)
	}
	if repository.permanents[0].cause != `unknown subject type "price_alert"` {
		t.Errorf("cause = %q, want the unknown-type cause", repository.permanents[0].cause)
	}
}

func TestWakeNeverBlocksWhenACycleIsAlreadyPending(t *testing.T) {
	job := NewJob(&fakeRepository{}, testLogger())
	job.Wake()
	job.Wake()
	if len(job.wake) != 1 {
		t.Fatalf("wake buffer = %d, want a single absorbed signal", len(job.wake))
	}
}

func TestRunStopsAfterCancellation(t *testing.T) {
	job := NewJob(&fakeRepository{claimed: []models.ClaimedJob{{ID: uuid.New(), SubjectType: "article", SubjectID: uuid.New()}}}, testLogger())

	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() { result <- job.Run(ctx) }()
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run error = %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not stop after cancellation")
	}
}
