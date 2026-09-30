package models

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrSourceConfigurationNotFound marks a missing configuration lookup;
// repositories translate the driver's no-rows error into it.
var ErrSourceConfigurationNotFound = errors.New("source configuration not found")

type Source struct {
	ID        uuid.UUID
	Name      string
	Type      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type SourceConfiguration struct {
	ID        uuid.UUID
	SourceID  uuid.UUID
	Type      string
	Config    json.RawMessage
	CreatedAt time.Time
	UpdatedAt time.Time
}
