package models

import (
	"time"

	"github.com/google/uuid"
)

type Entity struct {
	ID        uuid.UUID
	Code      string
	Name      string
	Type      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type EntityPair struct {
	ID            uuid.UUID
	BaseEntityID  uuid.UUID
	QuoteEntityID uuid.UUID
	Symbol        string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Name and type together identify a term. EntityID is the zero UUID when
// the phrase names no entity.
type KnowledgeTerm struct {
	ID        uuid.UUID
	Name      string
	Type      string
	EntityID  uuid.UUID
	CreatedAt time.Time
	UpdatedAt time.Time
}

type UserAsset struct {
	UserID       uuid.UUID
	EntityPairID uuid.UUID
	CreatedAt    time.Time
}
