//go:build integration

package emaildelivery

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Rahmannugar/authlier/emailverification"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/database/testdb"
	emaildeliveryrepositories "github.com/Rahmannugar/macro-terminal/server/internal/infra/emaildelivery/repositories"
	"github.com/google/uuid"
)

func TestQueuedDeliveryIsEncryptedAndDelivered(t *testing.T) {
	pool := testdb.OpenMigratedDatabase(t)
	queue, err := NewQueue(pool, testSecret)
	if err != nil {
		t.Fatalf("NewQueue: %v", err)
	}
	err = queue.SendVerification(t.Context(), emailverification.Message{
		UserID:    "subject-1",
		Email:     "reader@example.com",
		Code:      "654321",
		ExpiresAt: time.Now().UTC().Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("SendVerification: %v", err)
	}

	var ciphertext []byte
	var deliveryID uuid.UUID
	err = pool.QueryRow(t.Context(),
		`SELECT id, encrypted_payload FROM email_deliveries WHERE template = $1`,
		TemplateVerification,
	).Scan(&deliveryID, &ciphertext)
	if err != nil {
		t.Fatalf("load delivery: %v", err)
	}
	if bytes.Contains(ciphertext, []byte("654321")) {
		t.Fatal("verification code is readable in the stored payload")
	}

	repository := emaildeliveryrepositories.NewRepository(pool)
	var outboxDeliveryID uuid.UUID
	var outboxStatus string
	err = pool.QueryRow(t.Context(),
		`SELECT (payload->>'delivery_id')::uuid, status FROM outbox WHERE type = 'email_delivery'`,
	).Scan(&outboxDeliveryID, &outboxStatus)
	if err != nil {
		t.Fatalf("load outbox row: %v", err)
	}
	if outboxDeliveryID != deliveryID || outboxStatus != "pending" {
		t.Errorf("outbox row = %s/%s, want the delivery id and pending status", outboxDeliveryID, outboxStatus)
	}

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		if !bytes.Contains(body, []byte("reader@example.com")) {
			t.Errorf("recipient missing from payload: %s", body)
		}
		if request.Header.Get("Idempotency-Key") != "email-delivery/"+deliveryID.String() {
			t.Errorf("idempotency key = %q", request.Header.Get("Idempotency-Key"))
		}
		response.WriteHeader(http.StatusCreated)
		_, _ = response.Write([]byte(`{"id":"email-provider-7"}`))
	}))
	defer server.Close()
	sender, err := NewResendSender(server.Client(), "re_test_key", "Macro Terminal <noreply@example.com>")
	if err != nil {
		t.Fatalf("NewResendSender: %v", err)
	}
	sender.endpoint = server.URL + "/emails"

	job := NewJob(repository, queue, sender, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := job.cycle(t.Context()); err != nil {
		t.Fatalf("cycle: %v", err)
	}

	var status, providerID string
	var storedPayload []byte
	err = pool.QueryRow(t.Context(),
		`SELECT status, coalesce(provider_message_id, ''), encrypted_payload FROM email_deliveries WHERE id = $1`,
		deliveryID,
	).Scan(&status, &providerID, &storedPayload)
	if err != nil {
		t.Fatalf("load delivered row: %v", err)
	}
	if status != "delivered" || providerID != "email-provider-7" {
		t.Errorf("status/provider = %q/%q, want delivered/email-provider-7", status, providerID)
	}
	if len(storedPayload) != 1 || storedPayload[0] != 0 {
		t.Errorf("payload not wiped after delivery: %v", storedPayload)
	}

	err = pool.QueryRow(t.Context(),
		`SELECT status FROM outbox WHERE type = 'email_delivery'`,
	).Scan(&outboxStatus)
	if err != nil {
		t.Fatalf("load outbox row: %v", err)
	}
	if outboxStatus != "done" {
		t.Errorf("outbox status = %q, want done", outboxStatus)
	}

	again, err := repository.ClaimBatch(t.Context(), 8)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if len(again) != 0 {
		t.Errorf("second claim = %d rows, want none", len(again))
	}
}
