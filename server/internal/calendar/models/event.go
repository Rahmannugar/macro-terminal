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

type StoredEvent struct {
	ID          uuid.UUID  `json:"id"`
	SourceID    uuid.UUID  `json:"sourceId"`
	IndicatorID uuid.UUID  `json:"indicatorId"`
	ScheduledAt time.Time  `json:"scheduledAt"`
	ReleasedAt  *time.Time `json:"releasedAt"`
	Previous    *float64   `json:"previous"`
	Consensus   *float64   `json:"consensus"`
	Actual      *float64   `json:"actual"`
}
