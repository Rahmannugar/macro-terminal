package users

import (
	"context"
	"errors"

	"github.com/Rahmannugar/macro-terminal/server/internal/users/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	ErrUserNotFound  = errors.New("user not found")
	ErrInvalidFilter = errors.New("filter value is not valid")
)

const (
	AdminDefaultPageSize int32 = 25
	AdminMaximumPageSize int32 = 100
)

type AdminUsers interface {
	ListUsers(ctx context.Context, role *string, status *string, cursor *models.ListCursor, limit int32) ([]models.User, *models.ListCursor, error)
	UserByID(ctx context.Context, id uuid.UUID) (models.User, error)
	UpdateUserStatus(ctx context.Context, id uuid.UUID, status string) (models.User, error)
	UpdateRole(ctx context.Context, id uuid.UUID, role string) (models.User, error)
}

type UserPage struct {
	Users      []models.User
	NextCursor *models.ListCursor
}

type AdminService struct {
	users AdminUsers
}

func NewAdminService(users AdminUsers) *AdminService {
	return &AdminService{users: users}
}

func (service *AdminService) List(
	ctx context.Context,
	role *string,
	status *string,
	cursor *models.ListCursor,
	limit int32,
) (UserPage, error) {
	if role != nil && *role != models.RoleAdmin && *role != models.RoleUser {
		return UserPage{}, ErrInvalidFilter
	}
	if status != nil && *status != models.StatusActive && *status != models.StatusSuspended {
		return UserPage{}, ErrInvalidFilter
	}
	users, next, err := service.users.ListUsers(ctx, role, status, cursor, limit)
	if err != nil {
		return UserPage{}, err
	}
	return UserPage{Users: users, NextCursor: next}, nil
}

func (service *AdminService) Suspend(ctx context.Context, id uuid.UUID) (models.User, error) {
	return service.setStatus(ctx, id, models.StatusSuspended)
}

func (service *AdminService) Reactivate(ctx context.Context, id uuid.UUID) (models.User, error) {
	return service.setStatus(ctx, id, models.StatusActive)
}

func (service *AdminService) Promote(ctx context.Context, id uuid.UUID) (models.User, error) {
	user, err := service.user(ctx, id)
	if err != nil {
		return models.User{}, err
	}
	if user.Role == models.RoleAdmin {
		return user, nil
	}
	return service.users.UpdateRole(ctx, id, models.RoleAdmin)
}

func (service *AdminService) setStatus(ctx context.Context, id uuid.UUID, status string) (models.User, error) {
	user, err := service.user(ctx, id)
	if err != nil {
		return models.User{}, err
	}
	if user.Status == status {
		return user, nil
	}
	return service.users.UpdateUserStatus(ctx, id, status)
}

func (service *AdminService) user(ctx context.Context, id uuid.UUID) (models.User, error) {
	user, err := service.users.UserByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.User{}, ErrUserNotFound
	}
	if err != nil {
		return models.User{}, err
	}
	return user, nil
}
