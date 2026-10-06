package models

import (
	"time"

	"github.com/google/uuid"
)

const (
	StatusPending    = "pending"
	StatusProcessing = "processing"
	StatusDone       = "done"
	StatusFailed     = "failed"
)

type Job struct {
	ID        uuid.UUID
	Type      string
	Status    string
	Attempts  int32
	LastError *string
	CreatedAt time.Time
	UpdatedAt time.Time
}
