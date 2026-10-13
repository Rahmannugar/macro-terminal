package notification

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/notification/models"
	"github.com/Rahmannugar/macro-terminal/server/internal/notification/webhook"
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

	webhookClaimCalls  int
	webhookClaimed     []models.WebhookClaim
	webhookDeliveries  map[uuid.UUID]models.WebhookDelivery
	webhookDeliveryErr error
	webhookCompleted   []uuid.UUID
	webhookFailures    []failureRecord
	webhookPermanents  []failureRecord
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

func (repository *fakeRepository) EnqueueWebhookDeliveries(context.Context, int32) (int64, error) {
	return 0, nil
}

func (repository *fakeRepository) ReclaimStaleWebhookJobs(context.Context) (int64, error) {
	return 0, nil
}

func (repository *fakeRepository) ClaimWebhookBatch(context.Context, int32) ([]models.WebhookClaim, error) {
	repository.webhookClaimCalls++
	return repository.webhookClaimed, nil
}

func (repository *fakeRepository) CompleteWebhookJob(_ context.Context, id uuid.UUID) error {
	repository.webhookCompleted = append(repository.webhookCompleted, id)
	return nil
}

func (repository *fakeRepository) FailWebhookJob(_ context.Context, id uuid.UUID, maxAttempts int32, cause string) error {
	repository.webhookFailures = append(repository.webhookFailures, failureRecord{id: id, maxAttempts: maxAttempts, cause: cause})
	return nil
}

func (repository *fakeRepository) FailWebhookJobPermanently(_ context.Context, id uuid.UUID, cause string) error {
	repository.webhookPermanents = append(repository.webhookPermanents, failureRecord{id: id, cause: cause})
	return nil
}

func (repository *fakeRepository) GetWebhookDelivery(_ context.Context, notificationID uuid.UUID) (models.WebhookDelivery, error) {
	if repository.webhookDeliveryErr != nil {
		return models.WebhookDelivery{}, repository.webhookDeliveryErr
	}
	delivery, ok := repository.webhookDeliveries[notificationID]
	if !ok {
		return models.WebhookDelivery{}, fmt.Errorf("notification %s: %w", notificationID, pgx.ErrNoRows)
	}
	return delivery, nil
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

	if err := NewJob(repository, testLogger(), nil, nil, "http://localhost:5173").cycle(t.Context()); err != nil {
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

	if err := NewJob(repository, testLogger(), nil, nil, "http://localhost:5173").cycle(t.Context()); err != nil {
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

	if err := NewJob(repository, testLogger(), nil, nil, "http://localhost:5173").cycle(t.Context()); err != nil {
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

	if err := NewJob(repository, testLogger(), nil, nil, "http://localhost:5173").cycle(t.Context()); err == nil {
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

	if err := NewJob(repository, testLogger(), nil, nil, "http://localhost:5173").cycle(t.Context()); err != nil {
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
	job := NewJob(&fakeRepository{}, testLogger(), nil, nil, "http://localhost:5173")
	job.Wake()
	job.Wake()
	if len(job.wake) != 1 {
		t.Fatalf("wake buffer = %d, want a single absorbed signal", len(job.wake))
	}
}

func TestRunStopsAfterCancellation(t *testing.T) {
	job := NewJob(&fakeRepository{claimed: []models.ClaimedJob{{ID: uuid.New(), SubjectType: "article", SubjectID: uuid.New()}}}, testLogger(), nil, nil, "http://localhost:5173")

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

func testCipher(t *testing.T) *webhook.Cipher {
	t.Helper()
	cipher, err := webhook.NewCipher(bytes.Repeat([]byte("n"), 32))
	if err != nil {
		t.Fatalf("new cipher: %v", err)
	}
	return cipher
}

func webhookDeliveryFixture(t *testing.T, cipher *webhook.Cipher, endpoint, channel string, notificationID uuid.UUID) models.WebhookDelivery {
	t.Helper()
	nonce, ciphertext, err := cipher.Encrypt(endpoint)
	if err != nil {
		t.Fatalf("encrypt endpoint: %v", err)
	}
	return models.WebhookDelivery{
		Title:       "Fed holds rates steady",
		AlertType:   "new_article",
		PairSymbol:  "BTC/USD",
		SubjectType: "article",
		SubjectID:   uuid.New(),
		Channel:     &channel,
		Nonce:       nonce,
		Ciphertext:  ciphertext,
	}
}

func TestCycleDeliversWebhook(t *testing.T) {
	received := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cipher := testCipher(t)
	jobID := uuid.New()
	notificationID := uuid.New()
	repository := &fakeRepository{
		webhookClaimed: []models.WebhookClaim{{ID: jobID, NotificationID: notificationID, Attempts: 1}},
		webhookDeliveries: map[uuid.UUID]models.WebhookDelivery{
			notificationID: webhookDeliveryFixture(t, cipher, server.URL, webhook.ChannelSlack, notificationID),
		},
	}

	if err := NewJob(repository, testLogger(), server.Client(), cipher, "http://localhost:5173").cycle(t.Context()); err != nil {
		t.Fatalf("cycle: %v", err)
	}

	select {
	case body := <-received:
		if !strings.Contains(body, "Macro Terminal") || !strings.Contains(body, "Fed holds rates steady") {
			t.Errorf("posted body = %q, want the Macro Terminal message", body)
		}
		if !strings.Contains(body, "BTC/USD") {
			t.Errorf("posted body = %q, want the pair symbol", body)
		}
		if !strings.Contains(body, "http://localhost:5173/articles/") {
			t.Errorf("posted body = %q, want the link back to the article page", body)
		}
	case <-time.After(time.Second):
		t.Fatal("no webhook request reached the endpoint")
	}
	if len(repository.webhookCompleted) != 1 || repository.webhookCompleted[0] != jobID {
		t.Errorf("webhook completed = %v, want [%v]", repository.webhookCompleted, jobID)
	}
	if len(repository.webhookFailures) != 0 || len(repository.webhookPermanents) != 0 {
		t.Errorf("failures = %v permanents = %v, want none", repository.webhookFailures, repository.webhookPermanents)
	}
}

func TestCycleCompletesWebhookWhenChannelDisconnected(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cipher := testCipher(t)
	jobID := uuid.New()
	notificationID := uuid.New()
	delivery := webhookDeliveryFixture(t, cipher, server.URL, webhook.ChannelDiscord, notificationID)
	delivery.Channel = nil
	repository := &fakeRepository{
		webhookClaimed: []models.WebhookClaim{{ID: jobID, NotificationID: notificationID, Attempts: 1}},
		webhookDeliveries: map[uuid.UUID]models.WebhookDelivery{
			notificationID: delivery,
		},
	}

	if err := NewJob(repository, testLogger(), server.Client(), cipher, "http://localhost:5173").cycle(t.Context()); err != nil {
		t.Fatalf("cycle: %v", err)
	}

	if requests.Load() != 0 {
		t.Errorf("endpoint requests = %d, want 0 for a disconnected channel", requests.Load())
	}
	if len(repository.webhookCompleted) != 1 || repository.webhookCompleted[0] != jobID {
		t.Errorf("webhook completed = %v, want [%v]", repository.webhookCompleted, jobID)
	}
}

func TestCycleRetriesWebhookOnServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	cipher := testCipher(t)
	jobID := uuid.New()
	notificationID := uuid.New()
	repository := &fakeRepository{
		webhookClaimed: []models.WebhookClaim{{ID: jobID, NotificationID: notificationID, Attempts: 1}},
		webhookDeliveries: map[uuid.UUID]models.WebhookDelivery{
			notificationID: webhookDeliveryFixture(t, cipher, server.URL, webhook.ChannelSlack, notificationID),
		},
	}

	if err := NewJob(repository, testLogger(), server.Client(), cipher, "http://localhost:5173").cycle(t.Context()); err == nil {
		t.Fatal("cycle succeeded, want the webhook error reported")
	}

	if len(repository.webhookFailures) != 1 {
		t.Fatalf("webhook failures = %v, want one backoff failure", repository.webhookFailures)
	}
	failure := repository.webhookFailures[0]
	if failure.id != jobID || failure.maxAttempts != webhookMaxAttempts {
		t.Errorf("failure = %+v, want the claimed row and the attempt budget", failure)
	}
	if !strings.Contains(failure.cause, "500") {
		t.Errorf("cause = %q, want the status code", failure.cause)
	}
	if len(repository.webhookCompleted) != 0 || len(repository.webhookPermanents) != 0 {
		t.Errorf("completed = %v permanents = %v, want none", repository.webhookCompleted, repository.webhookPermanents)
	}
}

func TestCycleFailsWebhookPermanentlyOnRejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "invalid payload", http.StatusBadRequest)
	}))
	defer server.Close()

	cipher := testCipher(t)
	jobID := uuid.New()
	notificationID := uuid.New()
	repository := &fakeRepository{
		webhookClaimed: []models.WebhookClaim{{ID: jobID, NotificationID: notificationID, Attempts: 1}},
		webhookDeliveries: map[uuid.UUID]models.WebhookDelivery{
			notificationID: webhookDeliveryFixture(t, cipher, server.URL, webhook.ChannelDiscord, notificationID),
		},
	}

	if err := NewJob(repository, testLogger(), server.Client(), cipher, "http://localhost:5173").cycle(t.Context()); err != nil {
		t.Fatalf("cycle: %v, a rejected webhook is handled as a permanent failure", err)
	}

	if len(repository.webhookPermanents) != 1 || repository.webhookPermanents[0].id != jobID {
		t.Fatalf("webhook permanents = %v, want the claimed row", repository.webhookPermanents)
	}
	if len(repository.webhookFailures) != 0 {
		t.Errorf("webhook failures = %v, want none for a 4xx rejection", repository.webhookFailures)
	}
}

func TestCycleFailsWebhookPermanentlyWhenNotificationMissing(t *testing.T) {
	cipher := testCipher(t)
	jobID := uuid.New()
	repository := &fakeRepository{
		webhookClaimed:     []models.WebhookClaim{{ID: jobID, NotificationID: uuid.New(), Attempts: 1}},
		webhookDeliveryErr: pgx.ErrNoRows,
	}

	if err := NewJob(repository, testLogger(), http.DefaultClient, cipher, "http://localhost:5173").cycle(t.Context()); err != nil {
		t.Fatalf("cycle: %v", err)
	}

	if len(repository.webhookPermanents) != 1 || repository.webhookPermanents[0].id != jobID {
		t.Fatalf("webhook permanents = %v, want the claimed row", repository.webhookPermanents)
	}
	if repository.webhookPermanents[0].cause != "notification not found" {
		t.Errorf("cause = %q, want the missing-notification cause", repository.webhookPermanents[0].cause)
	}
}

func TestCycleSkipsWebhookPhaseWithoutCipher(t *testing.T) {
	repository := &fakeRepository{
		webhookClaimed: []models.WebhookClaim{{ID: uuid.New(), NotificationID: uuid.New(), Attempts: 1}},
	}

	if err := NewJob(repository, testLogger(), nil, nil, "http://localhost:5173").cycle(t.Context()); err != nil {
		t.Fatalf("cycle: %v", err)
	}

	if repository.webhookClaimCalls != 0 {
		t.Fatalf("webhook claim calls = %d, want 0 without a cipher", repository.webhookClaimCalls)
	}
}
