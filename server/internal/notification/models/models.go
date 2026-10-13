package models

import (
	"time"

	"github.com/google/uuid"
)

type ClaimedJob struct {
	ID          uuid.UUID
	SubjectType string
	SubjectID   uuid.UUID
	Attempts    int32
}

type Subject struct {
	ID    uuid.UUID
	Title string
}

type UserNotification struct {
	ID          uuid.UUID
	Type        string
	SubjectType string
	SubjectID   uuid.UUID
	Title       string
	ReadAt      *time.Time
	CreatedAt   time.Time
	PairSymbol  string
}

type UserChannel struct {
	Channel     string
	WebhookHint *string
}

type WebhookClaim struct {
	ID             uuid.UUID
	NotificationID uuid.UUID
	Attempts       int32
}

type WebhookDelivery struct {
	Title      string
	AlertType  string
	PairSymbol string
	Channel    *string
	Nonce      []byte
	Ciphertext []byte
}
