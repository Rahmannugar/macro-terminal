package models

import (
	"time"

	"github.com/google/uuid"
)

type ClaimedJob struct {
	ID         uuid.UUID
	DeliveryID uuid.UUID
	Attempts   int32
}

type Delivery struct {
	ID         uuid.UUID
	Template   string
	Nonce      []byte
	Ciphertext []byte
	ExpiresAt  time.Time
	Status     string
}
