package authentication

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Rahmannugar/authlier"
	"github.com/Rahmannugar/macro-terminal/server/internal/users/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func authSession(id string, expiresAt time.Time) authlier.Session {
	return authlier.Session{
		ID:        id,
		SubjectID: "subject-1",
		CreatedAt: time.Now().Add(-time.Hour),
		ExpiresAt: expiresAt,
	}
}

func mustParseUUID(t *testing.T, value string) uuid.UUID {
	t.Helper()
	parsed, err := uuid.Parse(value)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", value, err)
	}
	return parsed
}

func TestGetAccountRefreshesSessionCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	expiresAt := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	users := &fakeUsers{user: models.User{
		ID:        mustParseUUID(t, "3f1d9b0e-0000-7000-8000-000000000001"),
		SubjectID: "subject-1",
		Role:      models.RoleUser,
		Status:    models.StatusActive,
	}}
	service := NewService(
		&fakeSessions{session: authSession("session-1", expiresAt)},
		users,
		&fakeCredentials{email: "reader@example.com"},
	)
	cookie := &http.Cookie{
		Name:     "macro_terminal_session",
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
		HttpOnly: true,
	}
	router := gin.New()
	RegisterRoutes(router, service, http.NotFoundHandler(), cookie)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/account", nil)
	request.AddCookie(&http.Cookie{Name: cookie.Name, Value: "session-token"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	setCookie := response.Header().Get("Set-Cookie")
	if !strings.Contains(setCookie, "session-token") {
		t.Errorf("Set-Cookie = %q, want the incoming session token", setCookie)
	}
	if !strings.Contains(setCookie, "Expires=") || !strings.Contains(setCookie, "2030") {
		t.Errorf("Set-Cookie = %q, want the extended expiry", setCookie)
	}
	if !strings.Contains(setCookie, "HttpOnly") {
		t.Errorf("Set-Cookie = %q, want HttpOnly", setCookie)
	}
}

func TestGetAccountWithoutCookieSendsNoSetCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := NewService(
		&fakeSessions{session: authSession("session-1", time.Now().Add(24*time.Hour))},
		&fakeUsers{user: models.User{
			ID:        mustParseUUID(t, "3f1d9b0e-0000-7000-8000-000000000002"),
			SubjectID: "subject-1",
			Role:      models.RoleUser,
			Status:    models.StatusActive,
		}},
		&fakeCredentials{email: "reader@example.com"},
	)
	cookie := &http.Cookie{Name: "macro_terminal_session", Path: "/"}
	router := gin.New()
	RegisterRoutes(router, service, http.NotFoundHandler(), cookie)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/account", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if setCookie := response.Header().Get("Set-Cookie"); setCookie != "" {
		t.Errorf("Set-Cookie = %q, want none without an incoming cookie", setCookie)
	}
}
