package notification

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
	"github.com/Rahmannugar/macro-terminal/server/internal/notification/models"
	"github.com/Rahmannugar/macro-terminal/server/internal/notification/webhook"
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

type fakeStore struct {
	notifications []models.UserNotification
	next          *paging.Cursor
	unread        int64

	gotUserID    uuid.UUID
	gotLimit     int32
	gotReadID    uuid.UUID
	readAllCalls int
	markReadErr  error

	channels     []models.UserChannel
	alertPairIDs []uuid.UUID

	ensureInAppCalls int

	gotConnectChan string
	gotConnectURL  string
	syncedChannel  string

	gotDisconnectChan string
	deleteChannelErr  error

	gotAlertsPair uuid.UUID
}

func (store *fakeStore) ListUserNotificationsPage(_ context.Context, userID uuid.UUID, _ *paging.Cursor, limit int32) ([]models.UserNotification, *paging.Cursor, error) {
	store.gotUserID = userID
	store.gotLimit = limit
	return store.notifications, store.next, nil
}

func (store *fakeStore) CountUserUnreadNotifications(context.Context, uuid.UUID) (int64, error) {
	return store.unread, nil
}

func (store *fakeStore) MarkUserNotificationRead(_ context.Context, id, _ uuid.UUID) (int64, error) {
	if store.markReadErr != nil {
		return 0, store.markReadErr
	}
	store.gotReadID = id
	return 1, nil
}

func (store *fakeStore) MarkAllUserNotificationsRead(context.Context, uuid.UUID) (int64, error) {
	store.allReadCalls()
	return 1, nil
}

func (store *fakeStore) allReadCalls() {
	store.readAllCalls++
}

func (store *fakeStore) ListUserAlertPairIDs(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return store.alertPairIDs, nil
}

func (store *fakeStore) ListUserNotificationChannels(context.Context, uuid.UUID) ([]models.UserChannel, error) {
	return store.channels, nil
}

func (store *fakeStore) EnsureInAppChannel(context.Context, uuid.UUID) error {
	store.ensureInAppCalls++
	return nil
}

func (store *fakeStore) UpsertWebhookChannel(_ context.Context, _ uuid.UUID, channel string, _ []byte, _ []byte, hint string) error {
	store.gotConnectChan = channel
	store.gotConnectURL = hint
	return nil
}

func (store *fakeStore) DeleteUserNotificationChannel(_ context.Context, _ uuid.UUID, channel string) (int64, error) {
	if store.deleteChannelErr != nil {
		return 0, store.deleteChannelErr
	}
	store.gotDisconnectChan = channel
	return 1, nil
}

func (store *fakeStore) UpsertPairAlertsForConfiguredChannels(_ context.Context, _ uuid.UUID, entityPairID uuid.UUID) (int64, error) {
	store.gotAlertsPair = entityPairID
	return 2, nil
}

func (store *fakeStore) DeletePairAlerts(_ context.Context, _ uuid.UUID, entityPairID uuid.UUID) (int64, error) {
	store.gotAlertsPair = entityPairID
	return 2, nil
}

func (store *fakeStore) DeleteChannelAlerts(context.Context, uuid.UUID, string) (int64, error) {
	return 0, nil
}

func (store *fakeStore) SyncChannelAlertsFromInApp(_ context.Context, _ uuid.UUID, channel string) (int64, error) {
	store.syncedChannel = channel
	return 2, nil
}

func guardedRouter(t *testing.T, store *fakeStore, cipher *webhook.Cipher) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	group := router.Group("/api/v1")
	group.Use(authentication.RequireUser(authentication.NewService(
		stubSessions{},
		stubGuardUsers{},
		stubCredentials{},
	)))
	RegisterRoutes(group, NewService(store, cipher))
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

func notificationFixture() models.UserNotification {
	return models.UserNotification{
		ID:          uuid.New(),
		Type:        "new_article",
		SubjectType: "article",
		SubjectID:   uuid.New(),
		Title:       "Fed holds rates steady",
		PairSymbol:  "BTCUSD",
		CreatedAt:   time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC),
	}
}

func TestNotificationsListReturnsPairContextAndCursor(t *testing.T) {
	item := notificationFixture()
	next := paging.Cursor{At: item.CreatedAt, ID: item.ID}
	store := &fakeStore{notifications: []models.UserNotification{item}, next: &next}
	router := guardedRouter(t, store, testCipher(t))

	recorder := perform(router, http.MethodGet, "/api/v1/notifications", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	var page notificationListResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode body %q: %v", recorder.Body.String(), err)
	}
	if len(page.Notifications) != 1 || page.Notifications[0].PairSymbol != "BTCUSD" {
		t.Errorf("notifications = %+v, want the seeded row with pair symbol", page.Notifications)
	}
	if page.NextCursor == nil {
		t.Error("nextCursor = nil, want the page cursor")
	}
	if store.gotUserID != testUserID {
		t.Errorf("userID = %s, want %s", store.gotUserID, testUserID)
	}
	if store.gotLimit != defaultPageSize {
		t.Errorf("limit = %d, want %d", store.gotLimit, defaultPageSize)
	}
}

func TestNotificationsListRejectsBadPageParameters(t *testing.T) {
	router := guardedRouter(t, &fakeStore{}, testCipher(t))

	recorder := perform(router, http.MethodGet, "/api/v1/notifications?limit=0", "")
	if recorder.Code != http.StatusBadRequest || decodeError(t, recorder).Error.Code != "invalid_limit" {
		t.Errorf("limit=0 → status/code = %d/%q, want 400 invalid_limit", recorder.Code, decodeError(t, recorder).Error.Code)
	}

	recorder = perform(router, http.MethodGet, "/api/v1/notifications?cursor=not-base64!!", "")
	if recorder.Code != http.StatusBadRequest || decodeError(t, recorder).Error.Code != "invalid_cursor" {
		t.Errorf("garbage cursor → status/code = %d/%q, want 400 invalid_cursor", recorder.Code, decodeError(t, recorder).Error.Code)
	}
}

func TestNotificationsRequireSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	group := router.Group("/api/v1")
	group.Use(authentication.RequireUser(authentication.NewService(
		stubSessions{err: context.DeadlineExceeded},
		stubGuardUsers{},
		stubCredentials{},
	)))
	RegisterRoutes(group, NewService(&fakeStore{}, testCipher(t)))

	recorder := perform(router, http.MethodGet, "/api/v1/notifications", "")
	if recorder.Code != http.StatusUnauthorized || decodeError(t, recorder).Error.Code != "unauthenticated" {
		t.Errorf("status/code = %d/%q, want 401 unauthenticated", recorder.Code, decodeError(t, recorder).Error.Code)
	}
}

func TestUnreadCountReturnsCount(t *testing.T) {
	router := guardedRouter(t, &fakeStore{unread: 3}, testCipher(t))

	recorder := perform(router, http.MethodGet, "/api/v1/notifications/unread-count", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	var payload unreadCountResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if payload.Count != 3 {
		t.Errorf("count = %d, want 3", payload.Count)
	}
}

func TestMarkReadAnswersNotFoundForMissingNotification(t *testing.T) {
	store := &fakeStore{markReadErr: ErrNotificationNotFound}
	router := guardedRouter(t, store, testCipher(t))

	recorder := perform(router, http.MethodPost, "/api/v1/notifications/"+uuid.NewString()+"/read", "")
	if recorder.Code != http.StatusNotFound || decodeError(t, recorder).Error.Code != "not_found" {
		t.Errorf("status/code = %d/%q, want 404 not_found", recorder.Code, decodeError(t, recorder).Error.Code)
	}
}

func TestMarkReadRejectsMalformedID(t *testing.T) {
	router := guardedRouter(t, &fakeStore{}, testCipher(t))

	recorder := perform(router, http.MethodPost, "/api/v1/notifications/not-a-uuid/read", "")
	if recorder.Code != http.StatusBadRequest || decodeError(t, recorder).Error.Code != "invalid_id" {
		t.Errorf("status/code = %d/%q, want 400 invalid_id", recorder.Code, decodeError(t, recorder).Error.Code)
	}
}

func TestMarkReadReturnsNoContent(t *testing.T) {
	store := &fakeStore{}
	router := guardedRouter(t, store, testCipher(t))

	recorder := perform(router, http.MethodPost, "/api/v1/notifications/"+uuid.NewString()+"/read", "")
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d body = %s, want 204", recorder.Code, recorder.Body.String())
	}
	if store.gotReadID == uuid.Nil {
		t.Error("read id not forwarded")
	}
}

func TestMarkAllReadReturnsNoContent(t *testing.T) {
	store := &fakeStore{}
	router := guardedRouter(t, store, testCipher(t))

	recorder := perform(router, http.MethodPost, "/api/v1/notifications/read-all", "")
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d body = %s, want 204", recorder.Code, recorder.Body.String())
	}
	if store.readAllCalls != 1 {
		t.Errorf("read-all calls = %d, want 1", store.readAllCalls)
	}
}

func TestNotificationSettingsReturnChannelsAndAlerts(t *testing.T) {
	hint := "hooks.slack.com/…ab12"
	pairID := uuid.New()
	store := &fakeStore{
		channels: []models.UserChannel{
			{Channel: "in-app"},
			{Channel: "slack", WebhookHint: &hint},
		},
		alertPairIDs: []uuid.UUID{pairID},
	}
	router := guardedRouter(t, store, testCipher(t))

	recorder := perform(router, http.MethodGet, "/api/v1/notification-settings", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	var payload Settings
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !payload.InApp {
		t.Error("inApp = false, want the in-app channel present")
	}
	if len(payload.Channels) != 1 || payload.Channels[0].Channel != "slack" || payload.Channels[0].Endpoint == nil {
		t.Errorf("channels = %+v, want the slack channel with its masked endpoint", payload.Channels)
	}
	if len(payload.AlertPairIDs) != 1 || payload.AlertPairIDs[0] != pairID {
		t.Errorf("alertPairIds = %v, want [%s]", payload.AlertPairIDs, pairID)
	}
}

func TestNotificationSettingsReturnEmptyArraysForAFreshAccount(t *testing.T) {
	router := guardedRouter(t, &fakeStore{}, testCipher(t))

	recorder := perform(router, http.MethodGet, "/api/v1/notification-settings", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"alertPairIds":[]`) ||
		!strings.Contains(recorder.Body.String(), `"channels":[]`) {
		t.Errorf("body = %s, want empty arrays rather than null", recorder.Body.String())
	}
}

func TestSetInAppChannelTogglesGlobalSwitch(t *testing.T) {
	store := &fakeStore{}
	router := guardedRouter(t, store, testCipher(t))

	recorder := perform(router, http.MethodPut, "/api/v1/notification-channels/in-app", `{"enabled":true}`)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d body = %s, want 204", recorder.Code, recorder.Body.String())
	}
	if store.ensureInAppCalls != 1 {
		t.Errorf("ensure in-app calls = %d, want 1", store.ensureInAppCalls)
	}

	recorder = perform(router, http.MethodPut, "/api/v1/notification-channels/in-app", `{"enabled":false}`)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("disable status = %d body = %s, want 204", recorder.Code, recorder.Body.String())
	}
	if store.gotDisconnectChan != "in-app" {
		t.Errorf("removed channel = %q, want in-app", store.gotDisconnectChan)
	}
}

func TestConnectWebhookValidatesChannelAndURL(t *testing.T) {
	router := guardedRouter(t, &fakeStore{}, testCipher(t))

	recorder := perform(router, http.MethodPost, "/api/v1/notification-channels", `{"channel":"email","webhookUrl":"https://example.test/hook"}`)
	if recorder.Code != http.StatusBadRequest || decodeError(t, recorder).Error.Code != "invalid_channel" {
		t.Errorf("status/code = %d/%q, want 400 invalid_channel", recorder.Code, decodeError(t, recorder).Error.Code)
	}

	recorder = perform(router, http.MethodPost, "/api/v1/notification-channels", `{"channel":"slack","webhookUrl":"http://evil.test/hook"}`)
	if recorder.Code != http.StatusBadRequest || decodeError(t, recorder).Error.Code != "invalid_webhook_url" {
		t.Errorf("status/code = %d/%q, want 400 invalid_webhook_url", recorder.Code, decodeError(t, recorder).Error.Code)
	}
}

func TestConnectWebhookStoresHintAndSyncsAlerts(t *testing.T) {
	store := &fakeStore{}
	router := guardedRouter(t, store, testCipher(t))

	recorder := perform(router, http.MethodPost, "/api/v1/notification-channels",
		`{"channel":"discord","webhookUrl":"https://discord.com/api/webhooks/1234567890/abcdefghijklmnopqrstuvwx-yz0123456789"}`)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d body = %s, want 204", recorder.Code, recorder.Body.String())
	}
	if store.gotConnectChan != "discord" {
		t.Errorf("connected channel = %q, want discord", store.gotConnectChan)
	}
	if store.gotConnectURL == "" || strings.Contains(store.gotConnectURL, "abcdefghijklmnop") {
		t.Errorf("hint = %q, want a masked endpoint that never echoes the secret path", store.gotConnectURL)
	}
	if store.syncedChannel != "discord" {
		t.Errorf("synced channel = %q, want discord", store.syncedChannel)
	}
}

func TestConnectWebhookUnavailableWithoutCipher(t *testing.T) {
	router := guardedRouter(t, &fakeStore{}, nil)

	recorder := perform(router, http.MethodPost, "/api/v1/notification-channels",
		`{"channel":"slack","webhookUrl":"https://hooks.slack.com/services/T000/B000/XXXX"}`)
	if recorder.Code != http.StatusServiceUnavailable || decodeError(t, recorder).Error.Code != "webhooks_unavailable" {
		t.Errorf("status/code = %d/%q, want 503 webhooks_unavailable", recorder.Code, decodeError(t, recorder).Error.Code)
	}
}

func TestDisconnectChannelRejectsUnknownChannel(t *testing.T) {
	router := guardedRouter(t, &fakeStore{}, testCipher(t))

	recorder := perform(router, http.MethodDelete, "/api/v1/notification-channels/carrier-pigeon", "")
	if recorder.Code != http.StatusBadRequest || decodeError(t, recorder).Error.Code != "invalid_channel" {
		t.Errorf("status/code = %d/%q, want 400 invalid_channel", recorder.Code, decodeError(t, recorder).Error.Code)
	}
}

func TestSetPairAlertsSubscribesAndMutes(t *testing.T) {
	pairID := uuid.New()
	store := &fakeStore{}
	router := guardedRouter(t, store, testCipher(t))

	recorder := perform(router, http.MethodPut, "/api/v1/notification-alerts",
		`{"entityPairId":"`+pairID.String()+`","enabled":true}`)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("subscribe status = %d body = %s, want 204", recorder.Code, recorder.Body.String())
	}
	if store.gotAlertsPair != pairID {
		t.Errorf("pair = %s, want %s", store.gotAlertsPair, pairID)
	}
	if store.ensureInAppCalls != 1 {
		t.Errorf("ensure in-app calls = %d, want 1 before subscribing", store.ensureInAppCalls)
	}

	recorder = perform(router, http.MethodPut, "/api/v1/notification-alerts",
		`{"entityPairId":"`+pairID.String()+`","enabled":false}`)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("mute status = %d body = %s, want 204", recorder.Code, recorder.Body.String())
	}
	if store.gotAlertsPair != pairID {
		t.Errorf("pair = %s, want %s on mute", store.gotAlertsPair, pairID)
	}
}

func TestSetPairAlertsRequiresPairID(t *testing.T) {
	router := guardedRouter(t, &fakeStore{}, testCipher(t))

	recorder := perform(router, http.MethodPut, "/api/v1/notification-alerts", `{"enabled":true}`)
	if recorder.Code != http.StatusBadRequest || decodeError(t, recorder).Error.Code != "invalid_request" {
		t.Errorf("status/code = %d/%q, want 400 invalid_request", recorder.Code, decodeError(t, recorder).Error.Code)
	}
}
