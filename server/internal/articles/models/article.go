package models

import (
	"time"

	"github.com/google/uuid"
)

// Article is a canonical source record: one row per source and canonical
// URL.
type Article struct {
	ID          uuid.UUID
	SourceID    uuid.UUID
	Title       string
	Content     string
	URL         string
	PublishedAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// PersistEntry is one normalized candidate together with its mapping
// outcome. EntityIDs is non-empty when the dictionary matched entities;
// QueueUnmapped is set when the dictionary ran and matched nothing. Both
// stay empty when no dictionary was available, in which case only the
// article row is stored.
type PersistEntry struct {
	SourceID      uuid.UUID
	Title         string
	Content       string
	URL           string
	PublishedAt   *time.Time
	EntityIDs     []uuid.UUID
	QueueUnmapped bool
}

// PersistStats counts what one storage pass changed, so a log line can
// show queue movement instead of just success.
type PersistStats struct {
	UnmappedQueued int
	Resolved       int
}

// StoredArticle is a hydrated article row joined with its source name.
type StoredArticle struct {
	ID          uuid.UUID  `json:"id"`
	SourceID    uuid.UUID  `json:"sourceId"`
	SourceName  string     `json:"sourceName"`
	Title       string     `json:"title"`
	Content     string     `json:"content"`
	URL         string     `json:"url"`
	PublishedAt *time.Time `json:"publishedAt"`
}

// RecentArticleRef is one feed row's identity and sort key before hydration.
type RecentArticleRef struct {
	ID     uuid.UUID
	SortAt time.Time
}
