package users

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/common/paging"
	"github.com/Rahmannugar/macro-terminal/server/internal/users/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type fakeAdminUsers struct {
	users      []models.User
	next       *paging.Cursor
	user       models.User
	listErr    error
	getErr     error
	updateErr  error
	gotRole    *string
	gotStatus  *string
	gotLimit   int32
	gotCursor  *paging.Cursor
	updatedTo  string
	updateCall int
	getCall    int
}

func (fake *fakeAdminUsers) ListUsers(_ context.Context, role *string, status *string, cursor *paging.Cursor, limit int32) ([]models.User, *paging.Cursor, error) {
	fake.gotRole = role
	fake.gotStatus = status
	fake.gotCursor = cursor
	fake.gotLimit = limit
	return fake.users, fake.next, fake.listErr
}

func (fake *fakeAdminUsers) UserByID(_ context.Context, id uuid.UUID) (models.User, error) {
	fake.getCall++
	if fake.getErr != nil {
		return models.User{}, fake.getErr
	}
	return fake.user, nil
}

func (fake *fakeAdminUsers) UpdateUserStatus(_ context.Context, id uuid.UUID, status string) (models.User, error) {
	if fake.updateErr != nil {
		return models.User{}, fake.updateErr
	}
	fake.updateCall++
	fake.updatedTo = status
	fake.user.Status = status
	return fake.user, nil
}

func (fake *fakeAdminUsers) UpdateRole(_ context.Context, id uuid.UUID, role string) (models.User, error) {
	if fake.updateErr != nil {
		return models.User{}, fake.updateErr
	}
	fake.updateCall++
	fake.updatedTo = role
	fake.user.Role = role
	return fake.user, nil
}

func TestListValidatesFiltersAndReturnsPage(t *testing.T) {
	fake := &fakeAdminUsers{
		users: []models.User{{ID: uuid.New(), Role: models.RoleUser, Status: models.StatusActive}},
	}
	service := NewAdminService(fake)
	role := "root"
	if _, err := service.List(context.Background(), &role, nil, nil, 25); !errors.Is(err, ErrInvalidFilter) {
		t.Errorf("role filter error = %v, want invalid filter", err)
	}
	status := "banned"
	if _, err := service.List(context.Background(), nil, &status, nil, 25); !errors.Is(err, ErrInvalidFilter) {
		t.Errorf("status filter error = %v, want invalid filter", err)
	}

	page, err := service.List(context.Background(), nil, nil, nil, 25)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Users) != 1 || page.NextCursor != nil {
		t.Errorf("page = %d users / next %v, want 1 user / no next cursor", len(page.Users), page.NextCursor)
	}
	if fake.gotLimit != 25 || fake.gotCursor != nil {
		t.Errorf("limit/cursor = %d/%v, want 25/none", fake.gotLimit, fake.gotCursor)
	}

	cursor := &paging.Cursor{At: time.Unix(0, 1759750000000000000).UTC(), ID: uuid.New()}
	next := &paging.Cursor{At: time.Unix(0, 1759740000000000000).UTC(), ID: uuid.New()}
	fake.next = next
	page, err = service.List(context.Background(), nil, nil, cursor, 10)
	if err != nil {
		t.Fatalf("List with cursor: %v", err)
	}
	if fake.gotCursor != cursor || fake.gotLimit != 10 {
		t.Errorf("cursor/limit passed = %v/%d, want the caller's cursor and 10", fake.gotCursor, fake.gotLimit)
	}
	if page.NextCursor != next {
		t.Errorf("next cursor = %v, want the repository's next cursor", page.NextCursor)
	}
}

func TestSuspendUpdatesActiveAccountOnce(t *testing.T) {
	fake := &fakeAdminUsers{user: models.User{ID: uuid.New(), Role: models.RoleUser, Status: models.StatusActive}}
	service := NewAdminService(fake)

	user, err := service.Suspend(context.Background(), fake.user.ID)
	if err != nil {
		t.Fatalf("Suspend: %v", err)
	}
	if user.Status != models.StatusSuspended || fake.updatedTo != models.StatusSuspended {
		t.Errorf("status = %q, want suspended", user.Status)
	}

	if _, err := service.Suspend(context.Background(), fake.user.ID); err != nil {
		t.Fatalf("second Suspend: %v", err)
	}
	if fake.updateCall != 1 {
		t.Errorf("update calls = %d, want 1 (idempotent)", fake.updateCall)
	}
}

func TestReactivateRestoresActiveStatus(t *testing.T) {
	fake := &fakeAdminUsers{user: models.User{ID: uuid.New(), Status: models.StatusSuspended}}
	service := NewAdminService(fake)

	user, err := service.Reactivate(context.Background(), fake.user.ID)
	if err != nil {
		t.Fatalf("Reactivate: %v", err)
	}
	if user.Status != models.StatusActive {
		t.Errorf("status = %q, want active", user.Status)
	}
}

func TestPromoteGrantsAdminRoleIdempotently(t *testing.T) {
	fake := &fakeAdminUsers{user: models.User{ID: uuid.New(), Role: models.RoleUser, Status: models.StatusActive}}
	service := NewAdminService(fake)

	user, err := service.Promote(context.Background(), fake.user.ID)
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}
	if user.Role != models.RoleAdmin || fake.updatedTo != models.RoleAdmin {
		t.Errorf("role = %q, want admin", user.Role)
	}

	if _, err := service.Promote(context.Background(), fake.user.ID); err != nil {
		t.Fatalf("second Promote: %v", err)
	}
	if fake.updateCall != 1 {
		t.Errorf("update calls = %d, want 1 (idempotent)", fake.updateCall)
	}
}

func TestMutationsRejectUnknownAccount(t *testing.T) {
	fake := &fakeAdminUsers{getErr: pgx.ErrNoRows}
	service := NewAdminService(fake)
	id := uuid.New()

	if _, err := service.Suspend(context.Background(), id); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("Suspend error = %v, want user not found", err)
	}
	if _, err := service.Promote(context.Background(), id); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("Promote error = %v, want user not found", err)
	}
	if fake.updateCall != 0 {
		t.Errorf("update calls = %d, want 0", fake.updateCall)
	}
}
