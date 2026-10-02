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
// the phrase names no entity; IndicatorID is the zero UUID when the phrase
// does not classify calendar events.
type KnowledgeTerm struct {
	ID          uuid.UUID
	Name        string
	Type        string
	EntityID    uuid.UUID
	IndicatorID uuid.UUID
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Indicator is an economic series that calendar events measure, tied to
// the entity it belongs to.
type Indicator struct {
	ID        uuid.UUID
	Name      string
	EntityID  uuid.UUID
	Type      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// IndicatorTerm is one phrase that classifies an event name to an
// indicator during calendar ingestion.
type IndicatorTerm struct {
	Name        string
	IndicatorID uuid.UUID
}

type UserAsset struct {
	UserID       uuid.UUID
	EntityPairID uuid.UUID
	CreatedAt    time.Time
}
