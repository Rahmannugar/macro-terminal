package models

import "github.com/google/uuid"

type ClaimedJob struct {
	ID        uuid.UUID
	ArticleID uuid.UUID
	Attempts  int32
}

type Article struct {
	ID      uuid.UUID
	Title   string
	Content string
}
