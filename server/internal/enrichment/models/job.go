package models

import (
	"encoding/json"

	"github.com/google/uuid"
)

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

type StoredEnrichment struct {
	ID        uuid.UUID       `json:"id"`
	ArticleID uuid.UUID       `json:"articleId"`
	Model     string          `json:"model"`
	Result    json.RawMessage `json:"result"`
}
