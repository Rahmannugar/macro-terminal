package authentication

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/openapi"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func RegisterRoutes(router gin.IRouter, service *Service, authlierHandler http.Handler, sessionCookie *http.Cookie) {
	auth := router.Group("/auth")
	auth.Any("/*path", gin.WrapH(authlierHandler))

	account := router.Group("/account")
	account.GET("", getAccount(service, sessionCookie))
	account.Any("/*path", gin.WrapH(authlierHandler))

	router.GET("/api/v1/account", getAccount(service, sessionCookie))
	router.PATCH("/api/v1/account", updateUsername(service))
}

type accountJSON struct {
	ID       uuid.UUID `json:"id" example:"9d1e4a63-8f6d-4b9c-1f4e-7a553f8b2a60"`
	Username *string   `json:"username" example:"macrofan"`
	Email    string    `json:"email" example:"reader@example.com"`
	Role     string    `json:"role" example:"user"`
	Status   string    `json:"status" example:"active"`
}

type sessionJSON struct {
	CreatedAt time.Time `json:"createdAt" example:"2026-10-06T09:00:00Z"`
	ExpiresAt time.Time `json:"expiresAt" example:"2026-10-13T09:00:00Z"`
}

type accountResponse struct {
	Session sessionJSON `json:"session"`
	Account accountJSON `json:"account"`
}

type updateUsernameRequest struct {
	Username string `json:"username" example:"macrofan"`
}

// @Summary Get the signed-in account.
// @Description Resolves the session cookie to the local account, creating the user projection on first use. The email comes from the identity provider.
// @Tags account
// @Success 200 {object} accountResponse "The account and its session."
// @Failure 401 {object} openapi.Error "No valid session exists."
// @Failure 403 {object} openapi.Error "The account is suspended."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/account [get]
func getAccount(service *Service, sessionCookie *http.Cookie) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		account, err := service.Resolve(ctx.Request)
		switch {
		case errors.Is(err, ErrUnauthenticated):
			writeFailure(ctx, http.StatusUnauthorized, "unauthenticated", "Sign in to access your Macro Terminal account.")
		case errors.Is(err, ErrSuspended):
			writeFailure(ctx, http.StatusForbidden, "account_suspended", "This account has been suspended.")
		case err != nil:
			slog.Default().ErrorContext(ctx.Request.Context(), "account resolution failed", "error", err)
			writeFailure(ctx, http.StatusInternalServerError, "account_load_failed", "The account could not be loaded. Try again shortly.")
		default:
			refreshSessionCookie(ctx, sessionCookie, account.Session.ExpiresAt)
			ctx.JSON(http.StatusOK, newAccountResponse(account))
		}
	}
}

// refreshSessionCookie restamps the browser cookie with the session's current
// expiry so an extended session outlives the original sign-in cookie.
func refreshSessionCookie(ctx *gin.Context, template *http.Cookie, expiresAt time.Time) {
	if template == nil {
		return
	}
	incoming, err := ctx.Request.Cookie(template.Name)
	if err != nil || incoming.Value == "" {
		return
	}
	refreshed := *template
	refreshed.Value = incoming.Value
	refreshed.Expires = expiresAt
	http.SetCookie(ctx.Writer, &refreshed)
}

// @Summary Set the account username.
// @Description Stores a display username on the signed-in account. The username is optional and is not used to sign in.
// @Tags account
// @Param body body updateUsernameRequest true "The username to store, 1 to 64 characters."
// @Success 200 {object} accountResponse "The updated account and its session."
// @Failure 400 {object} openapi.Error "The username is missing or too long."
// @Failure 401 {object} openapi.Error "No valid session exists."
// @Failure 403 {object} openapi.Error "The account is suspended."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/account [patch]
func updateUsername(service *Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var request updateUsernameRequest
		if err := ctx.ShouldBindJSON(&request); err != nil {
			writeFailure(ctx, http.StatusBadRequest, "invalid_request", "The request body must be valid JSON.")
			return
		}
		account, err := service.Resolve(ctx.Request)
		switch {
		case errors.Is(err, ErrUnauthenticated):
			writeFailure(ctx, http.StatusUnauthorized, "unauthenticated", "Sign in to access your Macro Terminal account.")
			return
		case errors.Is(err, ErrSuspended):
			writeFailure(ctx, http.StatusForbidden, "account_suspended", "This account has been suspended.")
			return
		case err != nil:
			slog.Default().ErrorContext(ctx.Request.Context(), "account resolution failed", "error", err)
			writeFailure(ctx, http.StatusInternalServerError, "account_load_failed", "The account could not be loaded. Try again shortly.")
			return
		}
		account, err = service.SetUsername(ctx.Request.Context(), account, request.Username)
		switch {
		case errors.Is(err, ErrInvalidUsername):
			writeFailure(ctx, http.StatusBadRequest, "invalid_username", "Username must be 1 to 64 characters.")
		case err != nil:
			slog.Default().ErrorContext(ctx.Request.Context(), "username update failed", "error", err)
			writeFailure(ctx, http.StatusInternalServerError, "account_update_failed", "The account could not be updated. Try again shortly.")
		default:
			ctx.JSON(http.StatusOK, newAccountResponse(account))
		}
	}
}

func newAccountResponse(account Account) accountResponse {
	return accountResponse{
		Session: sessionJSON{
			CreatedAt: account.Session.CreatedAt,
			ExpiresAt: account.Session.ExpiresAt,
		},
		Account: accountJSON{
			ID:       account.User.ID,
			Username: account.User.Username,
			Email:    account.Email,
			Role:     account.User.Role,
			Status:   account.User.Status,
		},
	}
}

func writeFailure(ctx *gin.Context, status int, code, message string) {
	ctx.JSON(status, openapi.Error{Error: openapi.ErrorDetail{Code: code, Message: message}})
}
