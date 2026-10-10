package models

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrEventNotFound = errors.New("calendar event not found")

// PersistEntry is one classified calendar event ready for storage. The
// indicator came from deterministic classification; the entity is the one
// the indicator belongs to.
type PersistEntry struct {
	SourceID    uuid.UUID
	IndicatorID uuid.UUID
	EntityID    uuid.UUID
	Name        string
	ScheduledAt time.Time
	ReleasedAt  *time.Time
	Previous    *float64
	Consensus   *float64
	Actual      *float64
	CountryCode string
	Currency    string
	Importance  string
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
	Name          string
	IndicatorName string
	IndicatorType string
}

// CreateEventEntry is an administrator-created event and the entities it
// links to; the links may be empty.
type CreateEventEntry struct {
	SourceID    uuid.UUID
	IndicatorID uuid.UUID
	Name        string
	ScheduledAt time.Time
	ReleasedAt  *time.Time
	Previous    *float64
	Consensus   *float64
	Actual      *float64
	EntityIDs   []uuid.UUID
}

// UpdateEventEntry is an administrator edit of one stored event's displayed
// name, schedule, and figures.
type UpdateEventEntry struct {
	ID          uuid.UUID
	Name        string
	ScheduledAt time.Time
	ReleasedAt  *time.Time
	Previous    *float64
	Consensus   *float64
	Actual      *float64
}

// EventRecord is one stored event with its write timestamps, as returned
// by admin create and list.
type EventRecord struct {
	ID          uuid.UUID
	SourceID    uuid.UUID
	IndicatorID uuid.UUID
	Name        string
	ScheduledAt time.Time
	ReleasedAt  *time.Time
	Previous    *float64
	Consensus   *float64
	Actual      *float64
	CountryCode string
	Currency    string
	Importance  string
	Revision    int32
	ArchivedAt  *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// EventRow is a scheduled event joined with the names and provider tags a
// calendar screen renders directly.
type EventRow struct {
	ID            uuid.UUID
	SourceID      uuid.UUID
	IndicatorID   uuid.UUID
	Name          string
	ScheduledAt   time.Time
	ReleasedAt    *time.Time
	Previous      *float64
	Consensus     *float64
	Actual        *float64
	CountryCode   string
	Currency      string
	Importance    string
	Revision      int32
	CreatedAt     time.Time
	UpdatedAt     time.Time
	IndicatorName string
	SourceName    string
}

// EventPageQuery selects one calendar page: upcoming rows at or after Now,
// released rows before it, narrowed by provider country and importance and
// optionally to the watcher's followed pairs.
type EventPageQuery struct {
	Now         time.Time
	Countries   []string
	Importances []string
	Watcher     *uuid.UUID
}
