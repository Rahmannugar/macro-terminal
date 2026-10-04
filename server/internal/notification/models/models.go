package models

import "github.com/google/uuid"

type ClaimedJob struct {
	ID          uuid.UUID
	SubjectType string
	SubjectID   uuid.UUID
	Attempts    int32
}

type Subject struct {
	ID    uuid.UUID
	Title string
}
