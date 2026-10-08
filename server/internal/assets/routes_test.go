package assets

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
	entitymodels "github.com/Rahmannugar/macro-terminal/server/internal/entities/models"
	entityservices "github.com/Rahmannugar/macro-terminal/server/internal/entities/services"
	"github.com/Rahmannugar/macro-terminal/server/internal/openapi"
	usersmodels "github.com/Rahmannugar/macro-terminal/server/internal/users/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

var testUserID = uuid.New()

type stubSessions struct {
	err error
}

func (stub stubSessions) ResolveSession(*http.Request) (authlier.Session, error) {
	if stub.err != nil {
		return authlier.Session{}, stub.err
	}
	return authlier.Session{
		ID:        "session-1",
		SubjectID: "subject-1",
		ExpiresAt: time.Now().Add(time.Hour),
	}, nil
}

type stubGuardUsers struct{}

func (stubGuardUsers) ResolveUser(context.Context, string, string) (usersmodels.User, error) {
	return usersmodels.User{ID: testUserID, Role: usersmodels.RoleUser, Status: usersmodels.StatusActive}, nil
}

func (stubGuardUsers) UpdateUsername(_ context.Context, _ uuid.UUID, _ *string) (usersmodels.User, error) {
	return usersmodels.User{}, nil
}

type stubCredentials struct{}

func (stubCredentials) FindBySubject(context.Context, string) (emailpassword.User, emailpassword.PasswordCredential, error) {
	return emailpassword.User{ID: "subject-1", Email: "reader@example.com"}, emailpassword.PasswordCredential{}, nil
}

func (stubCredentials) AddPassword(context.Context, string, string, time.Time) (emailpassword.User, error) {
	return emailpassword.User{}, nil
}

func (stubCredentials) ReplacePasswordHash(context.Context, string, string, string, time.Time) error {
	return nil
}

func (stubCredentials) RemovePassword(context.Context, string, string, time.Time) error { return nil }

type fakeWatchList struct {
	pairs []entitymodels.EntityPair
	next  *paging.Cursor

	gotUserID uuid.UUID
	gotLimit  int32

	followErr   error
	gotFollowID uuid.UUID
}

func (fake *fakeWatchList) PairsForUserPage(_ context.Context, userID uuid.UUID, _ *paging.Cursor, limit int32) ([]entitymodels.EntityPair, *paging.Cursor, error) {
	fake.gotUserID = userID
	fake.gotLimit = limit
	return fake.pairs, fake.next, nil
}

func (fake *fakeWatchList) Subscribe(_ context.Context, _, entityPairID uuid.UUID) error {
	fake.gotFollowID = entityPairID
	return fake.followErr
}

func (fake *fakeWatchList) Unsubscribe(context.Context, uuid.UUID, uuid.UUID) error { return nil }

type fakePairs struct {
	pairs []entitymodels.EntityPair
	next  *paging.Cursor
}

func (fake *fakePairs) ListEntityPairsPage(context.Context, *paging.Cursor, int32) ([]entitymodels.EntityPair, *paging.Cursor, error) {
	return fake.pairs, fake.next, nil
}

func guardedRouter(t *testing.T, watchList WatchList) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	group := router.Group("/api/v1")
	group.Use(authentication.RequireUser(authentication.NewService(
		stubSessions{},
		stubGuardUsers{},
		stubCredentials{},
	)))
	RegisterWatchListRoutes(group, watchList)
	return router
}

func perform(router *gin.Engine, method, target, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	router.ServeHTTP(recorder, request)
	return recorder
}

func decodeError(t *testing.T, recorder *httptest.ResponseRecorder) openapi.Error {
	t.Helper()
	var failure openapi.Error
	if err := json.Unmarshal(recorder.Body.Bytes(), &failure); err != nil {
		t.Fatalf("decode failure: %v", err)
	}
	return failure
}

func pairFixture() entitymodels.EntityPair {
	now := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	return entitymodels.EntityPair{
		ID:            uuid.New(),
		BaseEntityID:  uuid.New(),
		QuoteEntityID: uuid.New(),
		Symbol:        "EURUSD",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

func TestWatchListRejectsBadPageParameters(t *testing.T) {
	router := guardedRouter(t, &fakeWatchList{})

	recorder := perform(router, http.MethodGet, "/api/v1/assets?limit=0", "")
	if recorder.Code != http.StatusBadRequest || decodeError(t, recorder).Error.Code != "invalid_limit" {
		t.Errorf("limit=0 → status/code = %d/%q, want 400 invalid_limit", recorder.Code, decodeError(t, recorder).Error.Code)
	}

	recorder = perform(router, http.MethodGet, "/api/v1/assets?cursor=not-base64!!", "")
	if recorder.Code != http.StatusBadRequest || decodeError(t, recorder).Error.Code != "invalid_cursor" {
		t.Errorf("garbage cursor → status/code = %d/%q, want 400 invalid_cursor", recorder.Code, decodeError(t, recorder).Error.Code)
	}
}

func TestWatchListReturnsPairsForTheSignedInUser(t *testing.T) {
	pair := pairFixture()
	next := paging.Cursor{At: pair.CreatedAt, ID: pair.ID}
	fake := &fakeWatchList{pairs: []entitymodels.EntityPair{pair}, next: &next}
	router := guardedRouter(t, fake)

	recorder := perform(router, http.MethodGet, "/api/v1/assets", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	var page entityPairListResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode body %q: %v", recorder.Body.String(), err)
	}
	if len(page.EntityPairs) != 1 || page.EntityPairs[0].ID != pair.ID || page.EntityPairs[0].Symbol != "EURUSD" {
		t.Errorf("entityPairs = %+v, want the followed pair", page.EntityPairs)
	}
	if page.NextCursor == nil {
		t.Error("nextCursor = nil, want the page cursor")
	}
	if fake.gotUserID != testUserID {
		t.Errorf("userID = %s, want %s", fake.gotUserID, testUserID)
	}
	if fake.gotLimit != defaultPageSize {
		t.Errorf("limit = %d, want %d", fake.gotLimit, defaultPageSize)
	}
}

func TestWatchListRequiresSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	group := router.Group("/api/v1")
	group.Use(authentication.RequireUser(authentication.NewService(
		stubSessions{err: context.DeadlineExceeded},
		stubGuardUsers{},
		stubCredentials{},
	)))
	RegisterWatchListRoutes(group, &fakeWatchList{})

	recorder := perform(router, http.MethodGet, "/api/v1/assets", "")
	if recorder.Code != http.StatusUnauthorized || decodeError(t, recorder).Error.Code != "unauthenticated" {
		t.Errorf("status/code = %d/%q, want 401 unauthenticated", recorder.Code, decodeError(t, recorder).Error.Code)
	}
}

func TestFollowPairReturnsNoContent(t *testing.T) {
	pair := pairFixture()
	fake := &fakeWatchList{}
	router := guardedRouter(t, fake)

	recorder := perform(router, http.MethodPost, "/api/v1/assets", `{"entityPairId":"`+pair.ID.String()+`"}`)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d body = %s, want 204", recorder.Code, recorder.Body.String())
	}
	if fake.gotFollowID != pair.ID {
		t.Errorf("followed pair = %s, want %s", fake.gotFollowID, pair.ID)
	}
}

func TestFollowPairAnswersNotFoundForUnknownPair(t *testing.T) {
	fake := &fakeWatchList{followErr: entityservices.ErrEntityPairNotFound}
	router := guardedRouter(t, fake)

	recorder := perform(router, http.MethodPost, "/api/v1/assets", `{"entityPairId":"`+uuid.NewString()+`"}`)
	if recorder.Code != http.StatusNotFound || decodeError(t, recorder).Error.Code != "entity_pair_not_found" {
		t.Errorf("status/code = %d/%q, want 404 entity_pair_not_found", recorder.Code, decodeError(t, recorder).Error.Code)
	}
}

func TestUnfollowPairRejectsMalformedID(t *testing.T) {
	router := guardedRouter(t, &fakeWatchList{})

	recorder := perform(router, http.MethodDelete, "/api/v1/assets/not-a-uuid", "")
	if recorder.Code != http.StatusBadRequest || decodeError(t, recorder).Error.Code != "invalid_id" {
		t.Errorf("status/code = %d/%q, want 400 invalid_id", recorder.Code, decodeError(t, recorder).Error.Code)
	}
}

func TestCatalogReturnsPairs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pair := pairFixture()
	router := gin.New()
	RegisterCatalogRoutes(router, &fakePairs{pairs: []entitymodels.EntityPair{pair}})

	recorder := perform(router, http.MethodGet, "/api/v1/entity-pairs", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	var page entityPairListResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode body %q: %v", recorder.Body.String(), err)
	}
	if len(page.EntityPairs) != 1 || page.EntityPairs[0].Symbol != "EURUSD" {
		t.Errorf("entityPairs = %+v, want the catalog pair", page.EntityPairs)
	}
	if page.NextCursor != nil {
		t.Errorf("nextCursor = %q, want null on the final page", *page.NextCursor)
	}
}
