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

type ContentTarget struct {
	ArticleID         uuid.UUID
	ArticleURL        string
	SourceID          uuid.UUID
	ConfigurationID   uuid.UUID
	ConfigurationType string
	Config            json.RawMessage
	SourceName        string
	SourceType        string
}
