package events

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/authentication"
	"github.com/Rahmannugar/macro-terminal/server/internal/openapi"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

const (
	streamBlock    = 25 * time.Second
	streamLifetime = 30 * time.Minute
)

// RegisterEventsRoutes mounts the server-sent event stream at /events on the
// caller's group. The caller mounts the session guard on the group.
//
// @Summary Stream live feed invalidation signals.
// @Description Emits a server-sent event whenever new articles are stored. Reconnecting browsers resume from the Last-Event-ID header. The connection is closed after a maximum lifetime and the browser reconnects automatically.
// @Tags events
// @Produce text/event-stream
// @Success 200 {string} string "text/event-stream"
// @Failure 401 {object} openapi.Error "No valid session exists."
// @Router /api/v1/events [get]
func RegisterEventsRoutes(router gin.IRoutes, redisClient *redis.Client) {
	router.GET("/events", streamEvents(redisClient, streamBlock, streamLifetime))
}

// streamEvents emits invalidation signals for one connection. Each stored
// article batch is published to the event stream by the worker; this handler
// tails that stream and forwards feed signals. Reconnecting browsers pass the
// last delivered id back in the Last-Event-ID header, so no signal is lost
// while a client is briefly offline. A heartbeat comment keeps proxies from
// closing an idle stream, and the connection ends after its maximum lifetime
// so clients reconnect instead of holding sockets forever.
func streamEvents(redisClient *redis.Client, block, lifetime time.Duration) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if _, ok := authentication.UserAccount(ctx); !ok {
			writeFailure(ctx, http.StatusUnauthorized, "unauthenticated", "Sign in to access your Macro Terminal account.")
			return
		}
		headers := ctx.Writer.Header()
		headers.Set("Content-Type", "text/event-stream")
		headers.Set("Cache-Control", "no-cache")
		headers.Set("Connection", "keep-alive")
		headers.Set("X-Accel-Buffering", "no")
		ctx.Writer.WriteHeader(http.StatusOK)
		ctx.Writer.Flush()

		requestContext := ctx.Request.Context()
		lifetimeTimer := time.NewTimer(lifetime)
		defer lifetimeTimer.Stop()

		lastID := ctx.GetHeader("Last-Event-ID")
		if lastID == "" {
			lastID = "$"
		}
		for {
			messages, err := redisClient.XRead(requestContext, &redis.XReadArgs{
				Streams: []string{FeedStreamKey, lastID},
				Block:   block,
				Count:   32,
			}).Result()
			switch {
			case errors.Is(err, redis.Nil):
				if _, writeErr := fmt.Fprint(ctx.Writer, ": ping\n\n"); writeErr != nil {
					return
				}
				ctx.Writer.Flush()
			case err != nil:
				return
			default:
				for _, stream := range messages {
					for _, message := range stream.Messages {
						lastID = message.ID
						if message.Values["type"] != "feed" {
							continue
						}
						if _, writeErr := fmt.Fprintf(ctx.Writer, "id: %s\nevent: feed\ndata: {\"type\":\"feed\"}\n\n", message.ID); writeErr != nil {
							return
						}
						ctx.Writer.Flush()
					}
				}
			}
			select {
			case <-requestContext.Done():
				return
			case <-lifetimeTimer.C:
				return
			default:
			}
		}
	}
}

func writeFailure(ctx *gin.Context, status int, code, message string) {
	ctx.JSON(status, openapi.Error{Error: openapi.ErrorDetail{Code: code, Message: message}})
}
