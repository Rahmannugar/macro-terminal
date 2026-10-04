package models

import (
	"time"

	"github.com/google/uuid"
)

type ClaimedJob struct {
	ID        uuid.UUID
	ArticleID uuid.UUID
	Attempts  int32
}

type Article struct {
	ID          uuid.UUID
	Title       string
	Content     string
	PublishedAt *time.Time
}

type StoryCluster struct {
	ID    uuid.UUID `json:"id"`
	Title string    `json:"title"`
}
