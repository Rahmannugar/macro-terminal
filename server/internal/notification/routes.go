package notification

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/authentication"
	"github.com/Rahmannugar/macro-terminal/server/internal/common/paging"
	"github.com/Rahmannugar/macro-terminal/server/internal/notification/models"
	"github.com/Rahmannugar/macro-terminal/server/internal/openapi"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	defaultPageSize = 25
	maximumPageSize = 100
)

// RegisterRoutes mounts the signed-in user's notification surface. The caller
// mounts the session guard on the group.
func RegisterRoutes(router gin.IRoutes, service *Service) {
	router.GET("/notifications", listNotifications(service))
	router.GET("/notifications/unread-count", unreadCount(service))
	router.POST("/notifications/read-all", markAllRead(service))
	router.POST("/notifications/:id/read", markRead(service))
	router.GET("/notification-settings", getSettings(service))
	router.PUT("/notification-channels/in-app", setInAppChannel(service))
	router.POST("/notification-channels", connectWebhook(service))
	router.DELETE("/notification-channels/:channel", disconnectChannel(service))
	router.PUT("/notification-alerts", setPairAlerts(service))
}

type notificationJSON struct {
	ID          uuid.UUID  `json:"id" example:"3f9a1d4e-7b2c-4f6a-8d1e-2b5c9a7f0e31"`
	Type        string     `json:"type" example:"new_article"`
	SubjectType string     `json:"subjectType" example:"article"`
	SubjectID   uuid.UUID  `json:"subjectId" example:"9d1e4a63-8f6d-4b9c-1f4e-7a553f8b2a60"`
	Title       string     `json:"title" example:"Fed holds rates steady"`
	PairSymbol  string     `json:"pairSymbol" example:"BTCUSD"`
	ReadAt      *time.Time `json:"readAt,omitempty" example:"2026-10-13T09:30:00Z"`
	CreatedAt   time.Time  `json:"createdAt" example:"2026-10-13T09:00:00Z"`
}

type notificationListResponse struct {
	Notifications []notificationJSON `json:"notifications"`
	NextCursor    *string            `json:"nextCursor"`
}

type unreadCountResponse struct {
	Count int64 `json:"count" example:"3"`
}

type inAppChannelRequest struct {
	Enabled bool `json:"enabled"`
}

type connectWebhookRequest struct {
	Channel    string `json:"channel" example:"slack"`
	WebhookURL string `json:"webhookUrl" example:"https://hooks.slack.com/services/T00/B00/xxxx"`
}

type setPairAlertsRequest struct {
	EntityPairID uuid.UUID `json:"entityPairId" example:"01a0f258-a1a9-730d-905c-3473db186806"`
	Enabled      bool      `json:"enabled"`
}

func newNotificationJSON(item models.UserNotification) notificationJSON {
	return notificationJSON{
		ID:          item.ID,
		Type:        item.Type,
		SubjectType: item.SubjectType,
		SubjectID:   item.SubjectID,
		Title:       item.Title,
		PairSymbol:  item.PairSymbol,
		ReadAt:      item.ReadAt,
		CreatedAt:   item.CreatedAt,
	}
}

// @Summary List notifications.
// @Description Returns the signed-in account's in-app notifications newest first with the pair symbol for context. Pass the returned nextCursor to fetch the next page.
// @Tags notifications
// @Param limit query int false "Page size between 1 and 100. Defaults to 25."
// @Param cursor query string false "Opaque cursor returned as nextCursor by the previous page."
// @Success 200 {object} notificationListResponse "The notifications and the cursor for the next page, if any."
// @Failure 400 {object} openapi.Error "The limit or cursor is invalid."
// @Failure 401 {object} openapi.Error "No valid session exists."
// @Failure 403 {object} openapi.Error "The account is suspended."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/notifications [get]
func listNotifications(service *Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		account, ok := authentication.UserAccount(ctx)
		if !ok {
			writeFailure(ctx, http.StatusUnauthorized, "unauthenticated", "Sign in to access your Macro Terminal account.")
			return
		}
		limit, cursor, ok := parsePageQuery(ctx)
		if !ok {
			return
		}
		rows, next, err := service.ListPage(ctx.Request.Context(), account.User.ID, cursor, limit)
		if err != nil {
			writeFailure(ctx, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
			return
		}
		response := notificationListResponse{Notifications: make([]notificationJSON, 0, len(rows))}
		for _, row := range rows {
			response.Notifications = append(response.Notifications, newNotificationJSON(row))
		}
		if next != nil {
			encoded := paging.EncodeCursor(*next)
			response.NextCursor = &encoded
		}
		ctx.JSON(http.StatusOK, response)
	}
}

// @Summary Count unread notifications.
// @Description Returns how many in-app notifications the signed-in account has not read yet.
// @Tags notifications
// @Success 200 {object} unreadCountResponse "The unread count."
// @Failure 401 {object} openapi.Error "No valid session exists."
// @Failure 403 {object} openapi.Error "The account is suspended."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/notifications/unread-count [get]
func unreadCount(service *Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		account, ok := authentication.UserAccount(ctx)
		if !ok {
			writeFailure(ctx, http.StatusUnauthorized, "unauthenticated", "Sign in to access your Macro Terminal account.")
			return
		}
		count, err := service.UnreadCount(ctx.Request.Context(), account.User.ID)
		if err != nil {
			writeFailure(ctx, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
			return
		}
		ctx.JSON(http.StatusOK, unreadCountResponse{Count: count})
	}
}

// @Summary Mark one notification read.
// @Description Marks a single in-app notification as read. Notifications owned by other accounts answer 404.
// @Tags notifications
// @Param id path string true "The notification id (UUID)."
// @Success 204 "The notification is read."
// @Failure 400 {object} openapi.Error "The id is not a UUID."
// @Failure 401 {object} openapi.Error "No valid session exists."
// @Failure 403 {object} openapi.Error "The account is suspended."
// @Failure 404 {object} openapi.Error "The notification does not exist."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/notifications/{id}/read [post]
func markRead(service *Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		account, ok := authentication.UserAccount(ctx)
		if !ok {
			writeFailure(ctx, http.StatusUnauthorized, "unauthenticated", "Sign in to access your Macro Terminal account.")
			return
		}
		notificationID, err := uuid.Parse(ctx.Param("id"))
		if err != nil {
			writeFailure(ctx, http.StatusBadRequest, "invalid_id", "The notification id must be a UUID.")
			return
		}
		if err := service.MarkRead(ctx.Request.Context(), account.User.ID, notificationID); err != nil {
			if errors.Is(err, ErrNotificationNotFound) {
				writeFailure(ctx, http.StatusNotFound, "not_found", "That notification does not exist.")
				return
			}
			writeFailure(ctx, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
			return
		}
		ctx.Status(http.StatusNoContent)
	}
}

// @Summary Mark all notifications read.
// @Description Marks every unread in-app notification of the signed-in account as read.
// @Tags notifications
// @Success 204 "All notifications are read."
// @Failure 401 {object} openapi.Error "No valid session exists."
// @Failure 403 {object} openapi.Error "The account is suspended."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/notifications/read-all [post]
func markAllRead(service *Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		account, ok := authentication.UserAccount(ctx)
		if !ok {
			writeFailure(ctx, http.StatusUnauthorized, "unauthenticated", "Sign in to access your Macro Terminal account.")
			return
		}
		if err := service.MarkAllRead(ctx.Request.Context(), account.User.ID); err != nil {
			writeFailure(ctx, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
			return
		}
		ctx.Status(http.StatusNoContent)
	}
}

// @Summary Get notification settings.
// @Description Returns the in-app kill switch, connected webhook channels with masked endpoints, and the pair ids with alerts enabled.
// @Tags notifications
// @Success 200 {object} Settings "The current settings."
// @Failure 401 {object} openapi.Error "No valid session exists."
// @Failure 403 {object} openapi.Error "The account is suspended."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/notification-settings [get]
func getSettings(service *Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		account, ok := authentication.UserAccount(ctx)
		if !ok {
			writeFailure(ctx, http.StatusUnauthorized, "unauthenticated", "Sign in to access your Macro Terminal account.")
			return
		}
		settings, err := service.Settings(ctx.Request.Context(), account.User.ID)
		if err != nil {
			writeFailure(ctx, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
			return
		}
		ctx.JSON(http.StatusOK, settings)
	}
}

// @Summary Set the in-app channel switch.
// @Description Enables or disables in-app delivery globally. Per-pair subscriptions are kept either way.
// @Tags notifications
// @Success 204 "The switch is saved."
// @Failure 400 {object} openapi.Error "The request body is invalid."
// @Failure 401 {object} openapi.Error "No valid session exists."
// @Failure 403 {object} openapi.Error "The account is suspended."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/notification-channels/in-app [put]
func setInAppChannel(service *Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		account, ok := authentication.UserAccount(ctx)
		if !ok {
			writeFailure(ctx, http.StatusUnauthorized, "unauthenticated", "Sign in to access your Macro Terminal account.")
			return
		}
		var request inAppChannelRequest
		if err := ctx.ShouldBindJSON(&request); err != nil {
			writeFailure(ctx, http.StatusBadRequest, "invalid_request", "The request body must be valid JSON.")
			return
		}
		if err := service.SetInApp(ctx.Request.Context(), account.User.ID, request.Enabled); err != nil {
			writeFailure(ctx, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
			return
		}
		ctx.Status(http.StatusNoContent)
	}
}

// @Summary Connect a webhook channel.
// @Description Stores an encrypted Slack or Discord channel webhook URL and subscribes it to every pair that already has alerts. The URL is never returned again; only a masked endpoint is shown.
// @Tags notifications
// @Success 204 "The channel is connected."
// @Failure 400 {object} openapi.Error "The channel or webhook URL is invalid."
// @Failure 401 {object} openapi.Error "No valid session exists."
// @Failure 403 {object} openapi.Error "The account is suspended."
// @Failure 503 {object} openapi.Error "Webhook channels are not configured on this deployment."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/notification-channels [post]
func connectWebhook(service *Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		account, ok := authentication.UserAccount(ctx)
		if !ok {
			writeFailure(ctx, http.StatusUnauthorized, "unauthenticated", "Sign in to access your Macro Terminal account.")
			return
		}
		var request connectWebhookRequest
		if err := ctx.ShouldBindJSON(&request); err != nil {
			writeFailure(ctx, http.StatusBadRequest, "invalid_request", "The request body must be valid JSON.")
			return
		}
		err := service.ConnectWebhook(ctx.Request.Context(), account.User.ID, request.Channel, request.WebhookURL)
		switch {
		case errors.Is(err, ErrInvalidChannel):
			writeFailure(ctx, http.StatusBadRequest, "invalid_channel", "Channel must be slack or discord.")
		case errors.Is(err, ErrInvalidWebhookURL):
			writeFailure(ctx, http.StatusBadRequest, "invalid_webhook_url", "Paste the full https webhook URL of a Slack or Discord channel.")
		case errors.Is(err, ErrWebhooksUnavailable):
			writeFailure(ctx, http.StatusServiceUnavailable, "webhooks_unavailable", "Webhook channels are not configured on this deployment.")
		case err != nil:
			writeFailure(ctx, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		default:
			ctx.Status(http.StatusNoContent)
		}
	}
}

// @Summary Disconnect a notification channel.
// @Description Removes the channel and its alerts. Pending webhook deliveries for it are dropped by the worker.
// @Tags notifications
// @Param channel path string true "The channel: in-app, slack, or discord."
// @Success 204 "The channel is disconnected."
// @Failure 400 {object} openapi.Error "The channel is not valid."
// @Failure 401 {object} openapi.Error "No valid session exists."
// @Failure 403 {object} openapi.Error "The account is suspended."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/notification-channels/{channel} [delete]
func disconnectChannel(service *Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		account, ok := authentication.UserAccount(ctx)
		if !ok {
			writeFailure(ctx, http.StatusUnauthorized, "unauthenticated", "Sign in to access your Macro Terminal account.")
			return
		}
		if err := service.DisconnectChannel(ctx.Request.Context(), account.User.ID, ctx.Param("channel")); err != nil {
			if errors.Is(err, ErrInvalidChannel) {
				writeFailure(ctx, http.StatusBadRequest, "invalid_channel", "Channel must be in-app, slack, or discord.")
				return
			}
			writeFailure(ctx, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
			return
		}
		ctx.Status(http.StatusNoContent)
	}
}

// @Summary Set a pair's alert preference.
// @Description Subscribes or mutes the pair for the signed-in account on every configured channel. This is the per-pair toggle on the watch list.
// @Tags notifications
// @Success 204 "The preference is saved."
// @Failure 400 {object} openapi.Error "The request body is invalid."
// @Failure 401 {object} openapi.Error "No valid session exists."
// @Failure 403 {object} openapi.Error "The account is suspended."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/notification-alerts [put]
func setPairAlerts(service *Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		account, ok := authentication.UserAccount(ctx)
		if !ok {
			writeFailure(ctx, http.StatusUnauthorized, "unauthenticated", "Sign in to access your Macro Terminal account.")
			return
		}
		var request setPairAlertsRequest
		if err := ctx.ShouldBindJSON(&request); err != nil {
			writeFailure(ctx, http.StatusBadRequest, "invalid_request", "The request body must be valid JSON.")
			return
		}
		if request.EntityPairID == uuid.Nil {
			writeFailure(ctx, http.StatusBadRequest, "invalid_request", "A pair id is required.")
			return
		}
		if err := service.SetPairAlerts(ctx.Request.Context(), account.User.ID, request.EntityPairID, request.Enabled); err != nil {
			writeFailure(ctx, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
			return
		}
		ctx.Status(http.StatusNoContent)
	}
}

func parsePageQuery(ctx *gin.Context) (int32, *paging.Cursor, bool) {
	limit := int32(defaultPageSize)
	if raw := ctx.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > maximumPageSize {
			writeFailure(ctx, http.StatusBadRequest, "invalid_limit", "Limit must be a number between 1 and 100.")
			return 0, nil, false
		}
		limit = int32(parsed)
	}
	cursor, err := paging.DecodeCursor(ctx.Query("cursor"))
	if err != nil {
		writeFailure(ctx, http.StatusBadRequest, "invalid_cursor", "Use the next cursor returned by the previous page.")
		return 0, nil, false
	}
	return limit, cursor, true
}

func writeFailure(ctx *gin.Context, status int, code, message string) {
	ctx.JSON(status, openapi.Error{Error: openapi.ErrorDetail{Code: code, Message: message}})
}
