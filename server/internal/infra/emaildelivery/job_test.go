package emaildelivery

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/infra/emaildelivery/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var testSecret = []byte("test-email-delivery-secret-32-bytes!")

type fakeRepository struct {
	mu sync.Mutex

	delivery      models.Delivery
	deliveryErr   error
	claimed       []models.ClaimedJob
	reclaimed     int64
	claimErr      error
	reclaimErr    error
	completed     []uuid.UUID
	failParams    []failCall
	permanentFail []failCall
	terminal      []terminalCall
	noted         []noteCall
	delivered     []deliverCall
	markRows      int64
}

type failCall struct {
	ID                uuid.UUID
	MaxAttempts       int32
	Cause             string
	RetryAfterSeconds float64
}

type terminalCall struct {
	ID     uuid.UUID
	Status string
	Cause  string
}

type noteCall struct {
	ID    uuid.UUID
	Cause string
}

type deliverCall struct {
	ID         uuid.UUID
	ProviderID string
}

func (repository *fakeRepository) ReclaimStaleJobs(context.Context) (int64, error) {
	return repository.reclaimed, repository.reclaimErr
}

func (repository *fakeRepository) ClaimBatch(context.Context, int32) ([]models.ClaimedJob, error) {
	return repository.claimed, repository.claimErr
}

func (repository *fakeRepository) Complete(_ context.Context, id uuid.UUID) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.completed = append(repository.completed, id)
	return nil
}

func (repository *fakeRepository) Fail(
	_ context.Context,
	id uuid.UUID,
	maxAttempts int32,
	cause string,
	retryAfterSeconds float64,
) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.failParams = append(repository.failParams, failCall{
		ID: id, MaxAttempts: maxAttempts, Cause: cause, RetryAfterSeconds: retryAfterSeconds,
	})
	return nil
}

func (repository *fakeRepository) FailPermanently(_ context.Context, id uuid.UUID, cause string) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.permanentFail = append(repository.permanentFail, failCall{ID: id, Cause: cause})
	return nil
}

func (repository *fakeRepository) Delivery(context.Context, uuid.UUID) (models.Delivery, error) {
	if repository.deliveryErr != nil {
		return models.Delivery{}, repository.deliveryErr
	}
	return repository.delivery, nil
}

func (repository *fakeRepository) MarkDelivered(_ context.Context, id uuid.UUID, providerID string) (int64, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.delivered = append(repository.delivered, deliverCall{ID: id, ProviderID: providerID})
	if repository.markRows >= 0 {
		return repository.markRows, nil
	}
	return 1, nil
}

func (repository *fakeRepository) MarkTerminal(_ context.Context, id uuid.UUID, status string, cause string) (int64, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.terminal = append(repository.terminal, terminalCall{ID: id, Status: status, Cause: cause})
	return 1, nil
}

func (repository *fakeRepository) NoteError(_ context.Context, id uuid.UUID, cause string) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.noted = append(repository.noted, noteCall{ID: id, Cause: cause})
	return nil
}

func newUnitJob(t *testing.T, repository *fakeRepository, handler http.HandlerFunc) (*Job, func()) {
	t.Helper()
	queue, err := deriveQueueForTest()
	if err != nil {
		t.Fatalf("build queue: %v", err)
	}
	server := httptest.NewServer(handler)
	sender, err := NewResendSender(server.Client(), "re_test_key", "Macro Terminal <noreply@example.com>")
	if err != nil {
		t.Fatalf("NewResendSender: %v", err)
	}
	sender.endpoint = server.URL + "/emails"
	job := NewJob(repository, queue, sender, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return job, server.Close
}

func deriveQueueForTest() (*Queue, error) {
	aead, err := deriveAEAD(testSecret)
	if err != nil {
		return nil, err
	}
	return &Queue{aead: aead}, nil
}

func pendingDelivery(t *testing.T, queue *Queue) models.Delivery {
	t.Helper()
	deliveryID := uuid.New()
	ciphertext, nonce, err := sealTestPayload(queue, deliveryID, Payload{
		Recipient: "reader@example.com",
		Code:      "123456",
		ExpiresAt: time.Now().UTC().Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("seal payload: %v", err)
	}
	return models.Delivery{
		ID:         deliveryID,
		Template:   TemplateVerification,
		Nonce:      nonce,
		Ciphertext: ciphertext,
		ExpiresAt:  time.Now().UTC().Add(10 * time.Minute),
		Status:     "pending",
	}
}

func sealTestPayload(queue *Queue, deliveryID uuid.UUID, payload Payload) ([]byte, []byte, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, queue.aead.NonceSize())
	for i := range nonce {
		nonce[i] = byte(i)
	}
	return queue.aead.Seal(nil, nonce, encoded, deliveryID[:]), nonce, nil
}

func TestCycleDeliversQueuedEmail(t *testing.T) {
	queue, err := deriveQueueForTest()
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	repository := &fakeRepository{markRows: 1}
	repository.delivery = pendingDelivery(t, queue)
	repository.claimed = []models.ClaimedJob{{
		ID: uuid.New(), DeliveryID: repository.delivery.ID, Attempts: 1,
	}}

	var sent struct {
		From    string   `json:"from"`
		To      []string `json:"to"`
		Subject string   `json:"subject"`
		Text    string   `json:"text"`
	}
	job, shutdown := newUnitJob(t, repository, func(response http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		_ = json.Unmarshal(body, &sent)
		response.WriteHeader(http.StatusCreated)
		_, _ = response.Write([]byte(`{"id":"email-provider-9"}`))
	})
	defer shutdown()

	if err := job.cycle(t.Context()); err != nil {
		t.Fatalf("cycle: %v", err)
	}
	if len(repository.delivered) != 1 || repository.delivered[0].ProviderID != "email-provider-9" {
		t.Errorf("delivered = %+v, want one delivery with the provider id", repository.delivered)
	}
	if len(repository.completed) != 1 {
		t.Errorf("completed = %v, want the outbox row closed", repository.completed)
	}
	if len(repository.failParams) != 0 || len(repository.permanentFail) != 0 {
		t.Errorf("unexpected failure recorded: retry=%v permanent=%v", repository.failParams, repository.permanentFail)
	}
	if len(sent.To) != 1 || sent.To[0] != "reader@example.com" {
		t.Errorf("to = %v, want the delivery recipient", sent.To)
	}
	if !strings.Contains(sent.Text, "123456") {
		t.Errorf("text = %q, want the OTP code in the body", sent.Text)
	}
}

func TestCycleFailsExpiredDeliveryPermanently(t *testing.T) {
	queue, err := deriveQueueForTest()
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	repository := &fakeRepository{}
	repository.delivery = pendingDelivery(t, queue)
	repository.delivery.ExpiresAt = time.Now().UTC().Add(-time.Minute)
	repository.claimed = []models.ClaimedJob{{
		ID: uuid.New(), DeliveryID: repository.delivery.ID, Attempts: 1,
	}}

	job, shutdown := newUnitJob(t, repository, func(http.ResponseWriter, *http.Request) {
		t.Error("sender must not be called for an expired delivery")
	})
	defer shutdown()

	if err := job.cycle(t.Context()); err != nil {
		t.Fatalf("cycle: %v", err)
	}
	if len(repository.terminal) != 1 || repository.terminal[0].Status != "expired" {
		t.Errorf("terminal = %+v, want one expired mark", repository.terminal)
	}
	if len(repository.permanentFail) != 1 {
		t.Errorf("permanent failures = %v, want one", repository.permanentFail)
	}
	if len(repository.delivered) != 0 {
		t.Errorf("delivered = %v, want none", repository.delivered)
	}
}

func TestCycleBacksOffRetryableFailure(t *testing.T) {
	queue, err := deriveQueueForTest()
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	repository := &fakeRepository{markRows: 1}
	repository.delivery = pendingDelivery(t, queue)
	repository.claimed = []models.ClaimedJob{{
		ID: uuid.New(), DeliveryID: repository.delivery.ID, Attempts: 2,
	}}

	job, shutdown := newUnitJob(t, repository, func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Retry-After", "45")
		response.WriteHeader(http.StatusTooManyRequests)
		_, _ = response.Write([]byte(`{"name":"rate_limit_exceeded"}`))
	})
	defer shutdown()

	if err := job.cycle(t.Context()); err != nil && !errors.Is(err, context.Canceled) {
		var failure SendFailure
		if !errors.As(err, &failure) {
			t.Fatalf("cycle error = %v, want a retryable send failure", err)
		}
	}
	if len(repository.failParams) != 1 {
		t.Fatalf("failures = %v, want one backoff", repository.failParams)
	}
	if repository.failParams[0].MaxAttempts != maxAttempts {
		t.Errorf("max attempts = %d, want %d", repository.failParams[0].MaxAttempts, maxAttempts)
	}
	if repository.failParams[0].RetryAfterSeconds != 45 {
		t.Errorf("retry after = %v, want the provider's 45s", repository.failParams[0].RetryAfterSeconds)
	}
	if len(repository.permanentFail) != 0 {
		t.Errorf("permanent failures = %v, want none", repository.permanentFail)
	}
	if len(repository.noted) != 1 {
		t.Errorf("noted errors = %v, want the failure recorded on the delivery", repository.noted)
	}
}

func TestCycleFailsPermanentProviderRejection(t *testing.T) {
	queue, err := deriveQueueForTest()
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	repository := &fakeRepository{markRows: 1}
	repository.delivery = pendingDelivery(t, queue)
	repository.claimed = []models.ClaimedJob{{
		ID: uuid.New(), DeliveryID: repository.delivery.ID, Attempts: 1,
	}}

	job, shutdown := newUnitJob(t, repository, func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusUnauthorized)
		_, _ = response.Write([]byte(`{"name":"validation_error"}`))
	})
	defer shutdown()

	_ = job.cycle(t.Context())
	if len(repository.terminal) != 1 || repository.terminal[0].Status != "failed" {
		t.Errorf("terminal = %+v, want one failed mark", repository.terminal)
	}
	if len(repository.permanentFail) != 1 {
		t.Errorf("permanent failures = %v, want one", repository.permanentFail)
	}
	if len(repository.failParams) != 0 {
		t.Errorf("backoffs = %v, want none for a non-retryable rejection", repository.failParams)
	}
}

func TestCycleClosesOutboxBehindFinalizedDelivery(t *testing.T) {
	repository := &fakeRepository{markRows: 1}
	repository.delivery = models.Delivery{ID: uuid.New(), Status: "delivered", ExpiresAt: time.Now().UTC().Add(time.Minute)}
	repository.claimed = []models.ClaimedJob{{
		ID: uuid.New(), DeliveryID: repository.delivery.ID, Attempts: 2,
	}}

	job, shutdown := newUnitJob(t, repository, func(http.ResponseWriter, *http.Request) {
		t.Error("sender must not be called for a finalized delivery")
	})
	defer shutdown()

	if err := job.cycle(t.Context()); err != nil {
		t.Fatalf("cycle: %v", err)
	}
	if len(repository.completed) != 1 {
		t.Errorf("completed = %v, want the outbox row closed", repository.completed)
	}
	if len(repository.delivered) != 0 {
		t.Errorf("delivered = %v, want none", repository.delivered)
	}
}

func TestCycleFailsPermanentlyWhenDeliveryRowIsMissing(t *testing.T) {
	repository := &fakeRepository{deliveryErr: pgx.ErrNoRows}
	repository.claimed = []models.ClaimedJob{{
		ID: uuid.New(), DeliveryID: uuid.New(), Attempts: 1,
	}}

	job, shutdown := newUnitJob(t, repository, func(http.ResponseWriter, *http.Request) {
		t.Error("sender must not be called without a delivery row")
	})
	defer shutdown()

	if err := job.cycle(t.Context()); err != nil {
		t.Fatalf("cycle: %v", err)
	}
	if len(repository.permanentFail) != 1 {
		t.Errorf("permanent failures = %v, want one", repository.permanentFail)
	}
}
