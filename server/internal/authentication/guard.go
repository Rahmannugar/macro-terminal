package authentication

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/Rahmannugar/macro-terminal/server/internal/users/models"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// RequireUser resolves the session and lets only active accounts through.
// Mounted on the user route group; the resolved account is available to
// handlers through UserAccount.
func RequireUser(service *Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		account, ok := resolveActiveAccount(ctx, service)
		if !ok {
			return
		}
		ctx.Set(userAccountKey, account)
		ctx.Next()
	}
}

// RequireAdmin resolves the session and lets only active administrators
// through. Mounted once on the admin route group; the resolved account is
// available to handlers through AdminAccount.
func RequireAdmin(service *Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		account, ok := resolveActiveAccount(ctx, service)
		if !ok {
			return
		}
		if account.User.Role != models.RoleAdmin && account.User.Role != models.RoleSuperAdmin {
			writeFailure(ctx, http.StatusForbidden, "admin_required", "This action requires an administrator account.")
			ctx.Abort()
			return
		}
		ctx.Set(adminAccountKey, account)
		ctx.Next()
	}
}

func resolveActiveAccount(ctx *gin.Context, service *Service) (Account, bool) {
	var account Account
	var err error
	if ctx.FullPath() == "/api/v1/events" {
		account, err = service.Resolve(ctx.Request)
	} else {
		originalRequest := ctx.Request
		requestContext, span := otel.Tracer("github.com/Rahmannugar/macro-terminal/server/internal/authentication").Start(
			originalRequest.Context(), "auth.session.resolve",
		)
		ctx.Request = originalRequest.WithContext(requestContext)
		account, err = service.Resolve(ctx.Request)
		ctx.Request = originalRequest
		outcome := "success"
		switch {
		case errors.Is(err, ErrUnauthenticated):
			outcome = "unauthenticated"
		case errors.Is(err, ErrSuspended):
			outcome = "suspended"
		case err != nil:
			outcome = "error"
			span.SetStatus(codes.Error, "session_resolution_failed")
		}
		span.SetAttributes(attribute.String("auth.outcome", outcome))
		span.End()
	}
	switch {
	case errors.Is(err, ErrUnauthenticated):
		writeFailure(ctx, http.StatusUnauthorized, "unauthenticated", "Sign in to access your Macro Terminal account.")
		ctx.Abort()
	case errors.Is(err, ErrSuspended):
		writeFailure(ctx, http.StatusForbidden, "account_suspended", "This account has been suspended.")
		ctx.Abort()
	case err != nil:
		slog.Default().ErrorContext(ctx.Request.Context(), "account guard resolution failed", "error", err)
		writeFailure(ctx, http.StatusInternalServerError, "account_load_failed", "The account could not be loaded. Try again shortly.")
		ctx.Abort()
	default:
		return account, true
	}
	return Account{}, false
}

type adminAccountContextKey struct{}

var adminAccountKey = adminAccountContextKey{}

// AdminAccount returns the administrator account the guard resolved, when the
// request passed RequireAdmin.
func AdminAccount(ctx *gin.Context) (Account, bool) {
	account, ok := ctx.Get(adminAccountKey)
	if !ok {
		return Account{}, false
	}
	resolved, ok := account.(Account)
	return resolved, ok
}

type userAccountContextKey struct{}

var userAccountKey = userAccountContextKey{}

// UserAccount returns the account the guard resolved, when the request
// passed RequireUser.
func UserAccount(ctx *gin.Context) (Account, bool) {
	account, ok := ctx.Get(userAccountKey)
	if !ok {
		return Account{}, false
	}
	resolved, ok := account.(Account)
	return resolved, ok
}
