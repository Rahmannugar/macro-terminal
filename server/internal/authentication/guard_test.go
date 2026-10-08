package authentication

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Rahmannugar/authlier"
	"github.com/Rahmannugar/macro-terminal/server/internal/openapi"
	"github.com/Rahmannugar/macro-terminal/server/internal/users/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func guardRequest(t *testing.T, service *Service) (*httptest.ResponseRecorder, bool) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	reached := false
	router := gin.New()
	router.GET("/guarded", RequireAdmin(service), func(ctx *gin.Context) {
		reached = true
		ctx.Status(http.StatusOK)
	})
	request := httptest.NewRequest(http.MethodGet, "/guarded", nil)
	request.AddCookie(&http.Cookie{Name: "macro_terminal_session", Value: "session-token"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response, reached
}

func guardFailure(t *testing.T, recorder *httptest.ResponseRecorder) openapi.Error {
	t.Helper()
	var body openapi.Error
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode failure: %v", err)
	}
	return body
}

func TestRequireAdminRejectsMissingSession(t *testing.T) {
	service := NewService(
		&fakeSessions{err: errors.New("no session")},
		&fakeUsers{},
		&fakeCredentials{},
	)

	response, reached := guardRequest(t, service)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
	if reached {
		t.Error("downstream handler ran, want blocked")
	}
	if code := guardFailure(t, response).Error.Code; code != "unauthenticated" {
		t.Errorf("code = %q, want unauthenticated", code)
	}
}

func TestRequireAdminRejectsSuspendedAccount(t *testing.T) {
	service := NewService(
		&fakeSessions{session: authlier.Session{ID: "session-1", SubjectID: "subject-1"}},
		&fakeUsers{user: models.User{ID: uuid.New(), Role: models.RoleAdmin, Status: models.StatusSuspended}},
		&fakeCredentials{email: "admin@example.com"},
	)

	response, reached := guardRequest(t, service)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", response.Code)
	}
	if reached {
		t.Error("downstream handler ran, want blocked")
	}
	if code := guardFailure(t, response).Error.Code; code != "account_suspended" {
		t.Errorf("code = %q, want account_suspended", code)
	}
}

func TestRequireAdminRejectsNonAdministrator(t *testing.T) {
	service := NewService(
		&fakeSessions{session: authlier.Session{ID: "session-1", SubjectID: "subject-1"}},
		&fakeUsers{user: models.User{ID: uuid.New(), Role: models.RoleUser, Status: models.StatusActive}},
		&fakeCredentials{email: "reader@example.com"},
	)

	response, reached := guardRequest(t, service)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", response.Code)
	}
	if reached {
		t.Error("downstream handler ran, want blocked")
	}
	if code := guardFailure(t, response).Error.Code; code != "admin_required" {
		t.Errorf("code = %q, want admin_required", code)
	}
}

func TestRequireAdminLetsAdministratorThrough(t *testing.T) {
	adminID := uuid.New()
	service := NewService(
		&fakeSessions{session: authlier.Session{
			ID:        "session-1",
			SubjectID: "subject-1",
			ExpiresAt: time.Now().Add(time.Hour),
		}},
		&fakeUsers{user: models.User{ID: adminID, Role: models.RoleAdmin, Status: models.StatusActive}},
		&fakeCredentials{email: "admin@example.com"},
	)
	gin.SetMode(gin.TestMode)
	accountSeen := Account{}
	reached := false
	router := gin.New()
	router.GET("/guarded", RequireAdmin(service), func(ctx *gin.Context) {
		reached = true
		accountSeen, _ = AdminAccount(ctx)
		ctx.Status(http.StatusOK)
	})
	request := httptest.NewRequest(http.MethodGet, "/guarded", nil)
	request.AddCookie(&http.Cookie{Name: "macro_terminal_session", Value: "session-token"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if !reached {
		t.Fatal("downstream handler did not run, want reached")
	}
	if accountSeen.User.ID != adminID || accountSeen.User.Role != models.RoleAdmin {
		t.Errorf("AdminAccount = %+v, want the resolved administrator", accountSeen.User)
	}
}

func TestRequireUserLetsActiveUserThrough(t *testing.T) {
	userID := uuid.New()
	service := NewService(
		&fakeSessions{session: authlier.Session{
			ID:        "session-1",
			SubjectID: "subject-1",
			ExpiresAt: time.Now().Add(time.Hour),
		}},
		&fakeUsers{user: models.User{ID: userID, Role: models.RoleUser, Status: models.StatusActive}},
		&fakeCredentials{email: "reader@example.com"},
	)
	gin.SetMode(gin.TestMode)
	accountSeen := Account{}
	reached := false
	router := gin.New()
	router.GET("/guarded", RequireUser(service), func(ctx *gin.Context) {
		reached = true
		accountSeen, _ = UserAccount(ctx)
		ctx.Status(http.StatusOK)
	})
	request := httptest.NewRequest(http.MethodGet, "/guarded", nil)
	request.AddCookie(&http.Cookie{Name: "macro_terminal_session", Value: "session-token"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if !reached {
		t.Fatal("downstream handler did not run, want reached")
	}
	if accountSeen.User.ID != userID || accountSeen.User.Role != models.RoleUser {
		t.Errorf("UserAccount = %+v, want the resolved user", accountSeen.User)
	}
}
