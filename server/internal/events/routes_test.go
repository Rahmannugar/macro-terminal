package events

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Rahmannugar/authlier"
	"github.com/Rahmannugar/authlier/emailpassword"
	"github.com/Rahmannugar/macro-terminal/server/internal/authentication"
	usersmodels "github.com/Rahmannugar/macro-terminal/server/internal/users/models"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type eventStubSessions struct{}

func (eventStubSessions) ResolveSession(*http.Request) (authlier.Session, error) {
	return authlier.Session{
		ID:        "session-1",
		SubjectID: "subject-1",
		ExpiresAt: time.Now().Add(time.Hour),
	}, nil
}

type eventStubUsers struct{}

func (eventStubUsers) ResolveUser(context.Context, string, string) (usersmodels.User, error) {
	return usersmodels.User{ID: uuid.New(), Role: usersmodels.RoleUser, Status: usersmodels.StatusActive}, nil
}

func (eventStubUsers) UpdateUsername(_ context.Context, _ uuid.UUID, _ *string) (usersmodels.User, error) {
	return usersmodels.User{}, nil
}

type eventStubCredentials struct{}

func (eventStubCredentials) FindBySubject(context.Context, string) (emailpassword.User, emailpassword.PasswordCredential, error) {
	return emailpassword.User{ID: "subject-1", Email: "reader@example.com"}, emailpassword.PasswordCredential{}, nil
}

func (eventStubCredentials) AddPassword(context.Context, string, string, time.Time) (emailpassword.User, error) {
	return emailpassword.User{}, nil
}

func (eventStubCredentials) ReplacePasswordHash(context.Context, string, string, string, time.Time) error {
	return nil
}

func (eventStubCredentials) RemovePassword(context.Context, string, string, time.Time) error {
	return nil
}

func newEventRouter(t *testing.T, redisClient *redis.Client, block, lifetime time.Duration) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	group := router.Group("/api/v1")
	group.Use(authentication.RequireUser(authentication.NewService(
		eventStubSessions{},
		eventStubUsers{},
		eventStubCredentials{},
	)))
	group.GET("/events", streamEvents(redisClient, block, lifetime))
	return router
}

func newEventRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	server, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(server.Close)
	return redis.NewClient(&redis.Options{Addr: server.Addr()}), server
}

func TestStreamEmitsFeedSignal(t *testing.T) {
	redisClient, _ := newEventRedis(t)
	if err := redisClient.XAdd(t.Context(), &redis.XAddArgs{
		Stream: FeedStreamKey,
		Values: map[string]any{"type": "feed"},
	}).Err(); err != nil {
		t.Fatalf("seed signal: %v", err)
	}
	router := newEventRouter(t, redisClient, 10*time.Millisecond, 100*time.Millisecond)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	request.Header.Set("Last-Event-ID", "0")
	router.ServeHTTP(recorder, request)

	body := recorder.Body.String()
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "text/event-stream" {
		t.Fatalf("content type = %q, want text/event-stream", contentType)
	}
	if !strings.Contains(body, "event: feed") {
		t.Fatalf("body = %q, want a feed event", body)
	}
	if !strings.Contains(body, "data: {\"type\":\"feed\"}") {
		t.Fatalf("body = %q, want the feed payload", body)
	}
}

func TestStreamHeartbeatsWhileIdle(t *testing.T) {
	redisClient, _ := newEventRedis(t)
	router := newEventRouter(t, redisClient, 5*time.Millisecond, 50*time.Millisecond)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	router.ServeHTTP(recorder, request)

	if body := recorder.Body.String(); !strings.Contains(body, ": ping") {
		t.Fatalf("body = %q, want a heartbeat comment", body)
	}
}

func TestStreamSkipsUnknownSignalTypes(t *testing.T) {
	redisClient, _ := newEventRedis(t)
	if err := redisClient.XAdd(t.Context(), &redis.XAddArgs{
		Stream: FeedStreamKey,
		Values: map[string]any{"type": "notification"},
	}).Err(); err != nil {
		t.Fatalf("seed signal: %v", err)
	}
	router := newEventRouter(t, redisClient, 10*time.Millisecond, 100*time.Millisecond)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	request.Header.Set("Last-Event-ID", "0")
	router.ServeHTTP(recorder, request)

	if body := recorder.Body.String(); strings.Contains(body, "event: feed") {
		t.Fatalf("body = %q, must not forward non-feed signals", body)
	}
}

func TestFeedSignalerPublishesToEventStream(t *testing.T) {
	redisClient, _ := newEventRedis(t)
	signaler := NewFeedSignaler(redisClient)

	if err := signaler.NotifyFeedChanged(t.Context()); err != nil {
		t.Fatalf("publish: %v", err)
	}
	messages, err := redisClient.XRange(t.Context(), FeedStreamKey, "-", "+").Result()
	if err != nil {
		t.Fatalf("read stream: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("stream messages = %d, want 1", len(messages))
	}
	if messages[0].Values["type"] != "feed" {
		t.Fatalf("signal type = %v, want feed", messages[0].Values["type"])
	}
}

func TestFeedSignalerToleratesNilReceiver(t *testing.T) {
	var signaler *FeedSignaler
	if err := signaler.NotifyFeedChanged(t.Context()); err != nil {
		t.Fatalf("nil signaler must publish nothing, got %v", err)
	}
}
