package users

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Rahmannugar/authlier"
	"github.com/Rahmannugar/authlier/emailpassword"
	"github.com/Rahmannugar/macro-terminal/server/internal/authentication"
	"github.com/Rahmannugar/macro-terminal/server/internal/common/paging"
	"github.com/Rahmannugar/macro-terminal/server/internal/openapi"
	"github.com/Rahmannugar/macro-terminal/server/internal/users/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func adminRouter(t *testing.T, fake *fakeAdminUsers) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterAdminRoutes(router.Group("/api/v1/admin"), NewAdminService(fake))
	return router
}

func performAdminRequest(router *gin.Engine, method, target, body string) *httptest.ResponseRecorder {
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, target, reader)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func decodeError(t *testing.T, recorder *httptest.ResponseRecorder) openapi.Error {
	t.Helper()
	var body openapi.Error
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode failure: %v", err)
	}
	return body
}

func TestListUsersRejectsBadPageParameters(t *testing.T) {
	router := adminRouter(t, &fakeAdminUsers{})

	recorder := performAdminRequest(router, http.MethodGet, "/api/v1/admin/users?limit=0", "")
	if recorder.Code != http.StatusBadRequest || decodeError(t, recorder).Error.Code != "invalid_limit" {
		t.Errorf("status/code = %d/%q, want 400 invalid_limit", recorder.Code, decodeError(t, recorder).Error.Code)
	}

	recorder = performAdminRequest(router, http.MethodGet, "/api/v1/admin/users?cursor=not-base64!!", "")
	if recorder.Code != http.StatusBadRequest || decodeError(t, recorder).Error.Code != "invalid_cursor" {
		t.Errorf("status/code = %d/%q, want 400 invalid_cursor", recorder.Code, decodeError(t, recorder).Error.Code)
	}
}

func TestListUsersRejectsUnknownFilterValues(t *testing.T) {
	router := adminRouter(t, &fakeAdminUsers{})

	recorder := performAdminRequest(router, http.MethodGet, "/api/v1/admin/users?role=root", "")
	if recorder.Code != http.StatusBadRequest || decodeError(t, recorder).Error.Code != "invalid_filter" {
		t.Errorf("status/code = %d/%q, want 400 invalid_filter", recorder.Code, decodeError(t, recorder).Error.Code)
	}
}

func TestListUsersReturnsMatchingAccounts(t *testing.T) {
	fake := &fakeAdminUsers{
		users: []models.User{{
			ID:     uuid.New(),
			Role:   models.RoleAdmin,
			Status: models.StatusActive,
		}},
		next: &paging.Cursor{At: time.Unix(0, 1759750000000000000).UTC(), ID: uuid.New()},
	}
	router := adminRouter(t, fake)

	recorder := performAdminRequest(router, http.MethodGet, "/api/v1/admin/users?role=admin&status=active", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body userListResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Users) != 1 || body.Users[0].Role != models.RoleAdmin {
		t.Errorf("body = %+v, want one admin", body)
	}
	if body.NextCursor == nil {
		t.Fatal("nextCursor = nil, want the encoded cursor for the next page")
	}
	decoded, err := paging.DecodeCursor(*body.NextCursor)
	if err != nil || decoded == nil || *decoded != *fake.next {
		t.Errorf("decoded nextCursor = %v/%v, want the cursor the repository returned", decoded, err)
	}
	if fake.gotRole == nil || *fake.gotRole != models.RoleAdmin || fake.gotStatus == nil || *fake.gotStatus != models.StatusActive {
		t.Errorf("filters passed = %v/%v, want admin/active", fake.gotRole, fake.gotStatus)
	}
}

func TestSuspendUserValidatesIdAndAccount(t *testing.T) {
	router := adminRouter(t, &fakeAdminUsers{})

	recorder := performAdminRequest(router, http.MethodPost, "/api/v1/admin/users/not-a-uuid/suspend", "")
	if recorder.Code != http.StatusBadRequest || decodeError(t, recorder).Error.Code != "invalid_id" {
		t.Errorf("status/code = %d/%q, want 400 invalid_id", recorder.Code, decodeError(t, recorder).Error.Code)
	}

	missing := adminRouter(t, &fakeAdminUsers{getErr: pgx.ErrNoRows})
	recorder = performAdminRequest(missing, http.MethodPost, "/api/v1/admin/users/"+uuid.NewString()+"/suspend", "")
	if recorder.Code != http.StatusNotFound || decodeError(t, recorder).Error.Code != "not_found" {
		t.Errorf("status/code = %d/%q, want 404 not_found", recorder.Code, decodeError(t, recorder).Error.Code)
	}
}

func TestSuspendUserReturnsUpdatedAccount(t *testing.T) {
	id := uuid.New()
	router := adminRouter(t, &fakeAdminUsers{user: models.User{ID: id, Role: models.RoleUser, Status: models.StatusActive}})

	recorder := performAdminRequest(router, http.MethodPost, "/api/v1/admin/users/"+id.String()+"/suspend", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body adminUserResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.User.Status != models.StatusSuspended {
		t.Errorf("status = %q, want suspended", body.User.Status)
	}
}

type stubSessions struct{}

func (stubSessions) ResolveSession(*http.Request) (authlier.Session, error) {
	return authlier.Session{ID: "session-1", SubjectID: "subject-1"}, nil
}

type stubGuardUsers struct {
	user models.User
}

func (stub *stubGuardUsers) ResolveUser(context.Context, string, string) (models.User, error) {
	return stub.user, nil
}

func (stub *stubGuardUsers) UpdateUsername(_ context.Context, _ uuid.UUID, username *string) (models.User, error) {
	stub.user.Username = username
	return stub.user, nil
}

type stubCredentials struct{}

func (stubCredentials) FindBySubject(_ context.Context, subjectID string) (emailpassword.User, emailpassword.PasswordCredential, error) {
	return emailpassword.User{ID: subjectID, Email: "admin@example.com"}, emailpassword.PasswordCredential{}, nil
}

func (stubCredentials) AddPassword(context.Context, string, string, time.Time) (emailpassword.User, error) {
	return emailpassword.User{}, nil
}

func (stubCredentials) ReplacePasswordHash(context.Context, string, string, string, time.Time) error {
	return nil
}

func (stubCredentials) RemovePassword(context.Context, string, string, time.Time) error {
	return nil
}

func TestSuspendRefusesTheActingAdministrator(t *testing.T) {
	gin.SetMode(gin.TestMode)
	actor := models.User{ID: uuid.New(), Role: models.RoleAdmin, Status: models.StatusActive}
	service := authentication.NewService(stubSessions{}, &stubGuardUsers{user: actor}, stubCredentials{})
	adminService := NewAdminService(&fakeAdminUsers{user: models.User{ID: actor.ID, Status: models.StatusActive}})

	router := gin.New()
	group := router.Group("/api/v1/admin")
	group.Use(authentication.RequireAdmin(service))
	RegisterAdminRoutes(group, adminService)

	recorder := performAdminRequest(router, http.MethodPost, "/api/v1/admin/users/"+actor.ID.String()+"/suspend", "")
	if recorder.Code != http.StatusConflict || decodeError(t, recorder).Error.Code != "cannot_suspend_self" {
		t.Errorf("status/code = %d/%q, want 409 cannot_suspend_self", recorder.Code, decodeError(t, recorder).Error.Code)
	}

	other := uuid.New()
	recorder = performAdminRequest(router, http.MethodPost, "/api/v1/admin/users/"+other.String()+"/suspend", "")
	if recorder.Code != http.StatusOK {
		t.Errorf("suspend other status = %d, want 200", recorder.Code)
	}
}
