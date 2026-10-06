package authentication

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/Rahmannugar/macro-terminal/server/internal/users/models"
	"github.com/gin-gonic/gin"
)

// RequireAdmin resolves the session and lets only active administrators
// through. Mounted once on the admin route group; the resolved account is
// available to handlers through AdminAccount.
func RequireAdmin(service *Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		account, err := service.Resolve(ctx.Request)
		switch {
		case errors.Is(err, ErrUnauthenticated):
			writeFailure(ctx, http.StatusUnauthorized, "unauthenticated", "Sign in to access your Macro Terminal account.")
			ctx.Abort()
		case errors.Is(err, ErrSuspended):
			writeFailure(ctx, http.StatusForbidden, "account_suspended", "This account has been suspended.")
			ctx.Abort()
		case err != nil:
			slog.Default().ErrorContext(ctx.Request.Context(), "admin guard resolution failed", "error", err)
			writeFailure(ctx, http.StatusInternalServerError, "account_load_failed", "The account could not be loaded. Try again shortly.")
			ctx.Abort()
		case account.User.Role != models.RoleAdmin:
			writeFailure(ctx, http.StatusForbidden, "admin_required", "This action requires an administrator account.")
			ctx.Abort()
		default:
			ctx.Set(adminAccountKey, account)
			ctx.Next()
		}
	}
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
