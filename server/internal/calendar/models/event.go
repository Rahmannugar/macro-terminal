package models

import (
	"time"

	"github.com/google/uuid"
)

// PersistEntry is one classified calendar event ready for storage. The
// indicator came from deterministic classification; the entity is the one
// the indicator belongs to.
type PersistEntry struct {
	SourceID    uuid.UUID
	IndicatorID uuid.UUID
	EntityID    uuid.UUID
	ScheduledAt time.Time
	ReleasedAt  *time.Time
	Previous    *float64
	Consensus   *float64
	Actual      *float64
}

type PersistStats struct {
	Stored     int
	LinksAdded int
}
