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

// EventContext is one event read together with the indicator it measures.
type EventContext struct {
	StoredEvent
	IndicatorName string
	IndicatorType string
}

// CreateEventEntry is an administrator-created event and the entities it
// links to; the links may be empty.
type CreateEventEntry struct {
	SourceID    uuid.UUID
	IndicatorID uuid.UUID
	ScheduledAt time.Time
	ReleasedAt  *time.Time
	Previous    *float64
	Consensus   *float64
	Actual      *float64
	EntityIDs   []uuid.UUID
}

// EventRecord is one stored event with its write timestamps, as returned
// by admin create and list.
type EventRecord struct {
	ID          uuid.UUID
	SourceID    uuid.UUID
	IndicatorID uuid.UUID
	ScheduledAt time.Time
	ReleasedAt  *time.Time
	Previous    *float64
	Consensus   *float64
	Actual      *float64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// UpcomingEvent is a scheduled event joined with the names a calendar
// screen renders directly.
type UpcomingEvent struct {
	ID            uuid.UUID
	SourceID      uuid.UUID
	IndicatorID   uuid.UUID
	ScheduledAt   time.Time
	ReleasedAt    *time.Time
	Previous      *float64
	Consensus     *float64
	Actual        *float64
	CreatedAt     time.Time
	UpdatedAt     time.Time
	IndicatorName string
	SourceName    string
}
