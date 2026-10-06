package repositories

import (
	"context"
	"fmt"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/users/models"
	userdb "github.com/Rahmannugar/macro-terminal/server/internal/users/repositories/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type UserRepository struct {
	queries *userdb.Queries
}

func NewUserRepository(pool *pgxpool.Pool) *UserRepository {
	return &UserRepository{queries: userdb.New(pool)}
}

func (repository *UserRepository) ResolveUser(
	ctx context.Context,
	subjectID string,
	role string,
) (models.User, error) {
	row, err := repository.queries.ResolveUserByAuthlierSubjectID(ctx, userdb.ResolveUserByAuthlierSubjectIDParams{
		ID:                uuid.New(),
		AuthlierSubjectID: subjectID,
		Role:              role,
	})
	if err != nil {
		return models.User{}, fmt.Errorf("resolve user: %w", err)
	}
	return userFromRow(row.ID, row.Username, row.AuthlierSubjectID, row.Role, row.Status, row.CreatedAt.Time, row.UpdatedAt.Time), nil
}

func (repository *UserRepository) UserByID(ctx context.Context, id uuid.UUID) (models.User, error) {
	row, err := repository.queries.GetUserByID(ctx, id)
	if err != nil {
		return models.User{}, fmt.Errorf("user by ID: %w", err)
	}
	return userFromRow(row.ID, row.Username, row.AuthlierSubjectID, row.Role, row.Status, row.CreatedAt.Time, row.UpdatedAt.Time), nil
}

func (repository *UserRepository) UserBySubject(
	ctx context.Context,
	subjectID string,
) (models.User, error) {
	row, err := repository.queries.GetUserByAuthlierSubjectID(ctx, subjectID)
	if err != nil {
		return models.User{}, fmt.Errorf("user by subject: %w", err)
	}
	return userFromRow(row.ID, row.Username, row.AuthlierSubjectID, row.Role, row.Status, row.CreatedAt.Time, row.UpdatedAt.Time), nil
}

func (repository *UserRepository) UpdateUsername(
	ctx context.Context,
	id uuid.UUID,
	username *string,
) (models.User, error) {
	row, err := repository.queries.UpdateUsername(ctx, userdb.UpdateUsernameParams{ID: id, Username: username})
	if err != nil {
		return models.User{}, fmt.Errorf("update username: %w", err)
	}
	return userFromRow(row.ID, row.Username, row.AuthlierSubjectID, row.Role, row.Status, row.CreatedAt.Time, row.UpdatedAt.Time), nil
}

func (repository *UserRepository) UpdateRole(
	ctx context.Context,
	id uuid.UUID,
	role string,
) (models.User, error) {
	row, err := repository.queries.UpdateRole(ctx, userdb.UpdateRoleParams{ID: id, Role: role})
	if err != nil {
		return models.User{}, fmt.Errorf("update role: %w", err)
	}
	return userFromRow(row.ID, row.Username, row.AuthlierSubjectID, row.Role, row.Status, row.CreatedAt.Time, row.UpdatedAt.Time), nil
}

func (repository *UserRepository) UpdateUserStatus(
	ctx context.Context,
	id uuid.UUID,
	status string,
) (models.User, error) {
	row, err := repository.queries.UpdateUserStatus(ctx, userdb.UpdateUserStatusParams{ID: id, Status: status})
	if err != nil {
		return models.User{}, fmt.Errorf("update user status: %w", err)
	}
	return userFromRow(row.ID, row.Username, row.AuthlierSubjectID, row.Role, row.Status, row.CreatedAt.Time, row.UpdatedAt.Time), nil
}

func (repository *UserRepository) ListUsers(
	ctx context.Context,
	role *string,
	status *string,
	cursor *models.ListCursor,
	limit int32,
) ([]models.User, *models.ListCursor, error) {
	params := userdb.ListUsersParams{
		Role:     role,
		Status:   status,
		PageSize: limit + 1,
	}
	if cursor != nil {
		params.CursorCreatedAt = pgtype.Timestamptz{Time: cursor.CreatedAt, Valid: true}
		params.CursorID = pgtype.UUID{Bytes: cursor.ID, Valid: true}
	}
	rows, err := repository.queries.ListUsers(ctx, params)
	if err != nil {
		return nil, nil, fmt.Errorf("list users: %w", err)
	}
	users := make([]models.User, 0, min(len(rows), int(limit)))
	for index, row := range rows {
		if int32(index) == limit {
			last := users[len(users)-1]
			return users, &models.ListCursor{CreatedAt: last.CreatedAt, ID: last.ID}, nil
		}
		users = append(users, userFromRow(row.ID, row.Username, row.AuthlierSubjectID, row.Role, row.Status, row.CreatedAt.Time, row.UpdatedAt.Time))
	}
	return users, nil, nil
}

func userFromRow(
	id uuid.UUID,
	username *string,
	subjectID string,
	role string,
	status string,
	createdAt time.Time,
	updatedAt time.Time,
) models.User {
	return models.User{
		ID:        id,
		Username:  username,
		SubjectID: subjectID,
		Role:      role,
		Status:    status,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}
}
