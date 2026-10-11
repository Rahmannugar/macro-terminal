package models

import (
	"time"

	"github.com/google/uuid"
)

const (
	RoleSuperAdmin = "super_admin"
	RoleAdmin      = "admin"
	RoleUser       = "user"

	StatusActive    = "active"
	StatusSuspended = "suspended"
)

type User struct {
	ID        uuid.UUID
	Username  *string
	SubjectID string
	Role      string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}
