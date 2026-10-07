//go:build integration

package integration_test

import (
	"errors"
	"testing"

	"github.com/Rahmannugar/macro-terminal/server/internal/common/paging"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/database/testdb"
	"github.com/Rahmannugar/macro-terminal/server/internal/users"
	"github.com/Rahmannugar/macro-terminal/server/internal/users/models"
	userrepositories "github.com/Rahmannugar/macro-terminal/server/internal/users/repositories"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func seedAdminUser(t *testing.T, pool *pgxpool.Pool, role, status string) models.User {
	t.Helper()
	user := models.User{
		ID:        uuid.New(),
		SubjectID: "admin-list-" + uuid.NewString(),
		Role:      role,
		Status:    status,
	}
	_, err := pool.Exec(
		t.Context(),
		"INSERT INTO users (id, authlier_subject_id, role, status) VALUES ($1, $2, $3, $4)",
		user.ID, user.SubjectID, user.Role, user.Status,
	)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return user
}

func TestAdminUserListingAndMutations(t *testing.T) {
	pool := testdb.OpenMigratedDatabase(t)
	repository := userrepositories.NewUserRepository(pool)
	service := users.NewAdminService(repository)

	activeAdmin := seedAdminUser(t, pool, models.RoleAdmin, models.StatusActive)
	seedAdminUser(t, pool, models.RoleUser, models.StatusActive)
	seedAdminUser(t, pool, models.RoleUser, models.StatusSuspended)

	role := models.RoleAdmin
	all, next, err := repository.ListUsers(t.Context(), nil, nil, nil, 25)
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("unfiltered list = %d users, want 3", len(all))
	}
	if next != nil {
		t.Errorf("next cursor = %v, want nil when every row fits on one page", next)
	}
	admins, _, err := repository.ListUsers(t.Context(), &role, nil, nil, 25)
	if err != nil {
		t.Fatalf("list admins: %v", err)
	}
	if len(admins) != 1 || admins[0].ID != activeAdmin.ID {
		t.Errorf("admin list = %d rows, want just the seeded admin", len(admins))
	}
	status := models.StatusSuspended
	suspended, _, err := repository.ListUsers(t.Context(), nil, &status, nil, 25)
	if err != nil {
		t.Fatalf("list suspended: %v", err)
	}
	if len(suspended) != 1 {
		t.Errorf("suspended list = %d rows, want 1", len(suspended))
	}

	walked := walkUserPages(t, repository, 1)
	if len(walked) != 3 {
		t.Errorf("cursor walk = %d unique users, want 3", len(walked))
	}
	for _, user := range all {
		if !walked[user.ID] {
			t.Errorf("user %s missing from the cursor walk", user.ID)
		}
	}

	suspendedUser, err := service.Suspend(t.Context(), activeAdmin.ID)
	if err != nil {
		t.Fatalf("suspend: %v", err)
	}
	if suspendedUser.Status != models.StatusSuspended {
		t.Errorf("status = %q, want suspended", suspendedUser.Status)
	}
	if _, err := service.Suspend(t.Context(), activeAdmin.ID); err != nil {
		t.Fatalf("idempotent suspend: %v", err)
	}

	restored, err := service.Reactivate(t.Context(), activeAdmin.ID)
	if err != nil {
		t.Fatalf("reactivate: %v", err)
	}
	if restored.Status != models.StatusActive {
		t.Errorf("status = %q, want active", restored.Status)
	}

	promoted, err := service.Promote(t.Context(), activeAdmin.ID)
	if err != nil {
		t.Fatalf("promote existing admin: %v", err)
	}
	if promoted.Role != models.RoleAdmin {
		t.Errorf("role after idempotent promote = %q, want admin", promoted.Role)
	}

	if _, err := service.Suspend(t.Context(), uuid.New()); !errors.Is(err, users.ErrUserNotFound) {
		t.Errorf("unknown id error = %v, want not found", err)
	}
}

func walkUserPages(t *testing.T, repository *userrepositories.UserRepository, pageSize int32) map[uuid.UUID]bool {
	t.Helper()
	seen := make(map[uuid.UUID]bool)
	var cursor *paging.Cursor
	for pages := 0; ; pages++ {
		if pages > 10 {
			t.Fatal("cursor walk did not terminate")
		}
		page, next, err := repository.ListUsers(t.Context(), nil, nil, cursor, pageSize)
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		if len(page) == 0 && next != nil {
			t.Fatalf("page %d returned no rows but promised a next cursor", pages)
		}
		for _, user := range page {
			if seen[user.ID] {
				t.Fatalf("cursor walk repeated user %s", user.ID)
			}
			seen[user.ID] = true
		}
		if next == nil {
			return seen
		}
		cursor = next
	}
}
