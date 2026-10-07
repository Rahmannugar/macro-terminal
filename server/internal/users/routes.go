package users

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/authentication"
	"github.com/Rahmannugar/macro-terminal/server/internal/common/paging"
	"github.com/Rahmannugar/macro-terminal/server/internal/openapi"
	"github.com/Rahmannugar/macro-terminal/server/internal/users/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func RegisterAdminRoutes(router gin.IRoutes, service *AdminService) {
	router.GET("/users", listUsers(service))
	router.POST("/users/:id/suspend", suspendUser(service))
	router.POST("/users/:id/reactivate", reactivateUser(service))
	router.POST("/users/:id/promote", promoteUser(service))
}

type adminUserJSON struct {
	ID        uuid.UUID `json:"id" example:"9d1e4a63-8f6d-4b9c-1f4e-7a553f8b2a60"`
	Username  *string   `json:"username" example:"macrofan"`
	Role      string    `json:"role" example:"admin"`
	Status    string    `json:"status" example:"active"`
	CreatedAt time.Time `json:"createdAt" example:"2026-10-06T09:00:00Z"`
	UpdatedAt time.Time `json:"updatedAt" example:"2026-10-06T09:00:00Z"`
}

type userListResponse struct {
	Users      []adminUserJSON `json:"users"`
	NextCursor *string         `json:"nextCursor"`
}

type adminUserResponse struct {
	User adminUserJSON `json:"user"`
}

// @Summary List user accounts.
// @Description Returns accounts newest first with their role and status, optionally filtered by role or status. Pass the returned nextCursor to fetch the next page.
// @Tags admin
// @Param role query string false "Filter by role: admin or user."
// @Param status query string false "Filter by status: active or suspended."
// @Param limit query int false "Page size between 1 and 100. Defaults to 25."
// @Param cursor query string false "Opaque cursor returned as nextCursor by the previous page."
// @Success 200 {object} userListResponse "The accounts and the cursor for the next page, if any."
// @Failure 400 {object} openapi.Error "A filter, limit, or cursor is invalid."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/admin/users [get]
func listUsers(service *AdminService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		limit, ok := parseAdminLimit(ctx.Query("limit"))
		if !ok {
			writeFailure(ctx, http.StatusBadRequest, "invalid_limit", "Limit must be a number between 1 and 100.")
			return
		}
		cursor, err := paging.DecodeCursor(ctx.Query("cursor"))
		if err != nil {
			writeFailure(ctx, http.StatusBadRequest, "invalid_cursor", "Use the next cursor returned by the previous page.")
			return
		}
		role := optionalFilter(ctx.Query("role"))
		status := optionalFilter(ctx.Query("status"))

		page, err := service.List(ctx.Request.Context(), role, status, cursor, limit)
		switch {
		case errors.Is(err, ErrInvalidFilter):
			writeFailure(ctx, http.StatusBadRequest, "invalid_filter", "Role must be admin or user; status must be active or suspended.")
		case err != nil:
			slog.Default().ErrorContext(ctx.Request.Context(), "user list failed", "error", err)
			writeFailure(ctx, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		default:
			ctx.JSON(http.StatusOK, newUserListResponse(page))
		}
	}
}

var ErrSelfSuspend = errors.New("cannot suspend the acting administrator")

// @Summary Suspend a user account.
// @Description Marks the account suspended. Every request the account makes is refused with a suspension error from then on. Suspending an already suspended account changes nothing, and an administrator cannot suspend their own account.
// @Tags admin
// @Param id path string true "User id (UUID)."
// @Success 200 {object} adminUserResponse "The updated account."
// @Failure 400 {object} openapi.Error "The id is not a valid UUID."
// @Failure 404 {object} openapi.Error "No account has that id."
// @Failure 409 {object} openapi.Error "The administrator tried to suspend their own account."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/admin/users/{id}/suspend [post]
func suspendUser(service *AdminService) gin.HandlerFunc {
	return mutateUser(service, func(ctx *gin.Context, service *AdminService, id uuid.UUID) (models.User, error) {
		if actor, ok := authentication.AdminAccount(ctx); ok && actor.User.ID == id {
			return models.User{}, ErrSelfSuspend
		}
		return service.Suspend(ctx.Request.Context(), id)
	})
}

// @Summary Reactivate a suspended user account.
// @Description Returns the account to active status so its sessions work again. Reactivating an already active account changes nothing.
// @Tags admin
// @Param id path string true "User id (UUID)."
// @Success 200 {object} adminUserResponse "The updated account."
// @Failure 400 {object} openapi.Error "The id is not a valid UUID."
// @Failure 404 {object} openapi.Error "No account has that id."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/admin/users/{id}/reactivate [post]
func reactivateUser(service *AdminService) gin.HandlerFunc {
	return mutateUser(service, func(ctx *gin.Context, service *AdminService, id uuid.UUID) (models.User, error) {
		return service.Reactivate(ctx.Request.Context(), id)
	})
}

// @Summary Promote a user to administrator.
// @Description Grants the account full administrative access. Promoting an account that is already an administrator changes nothing. Administrators cannot be demoted.
// @Tags admin
// @Param id path string true "User id (UUID)."
// @Success 200 {object} adminUserResponse "The updated account."
// @Failure 400 {object} openapi.Error "The id is not a valid UUID."
// @Failure 404 {object} openapi.Error "No account has that id."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/admin/users/{id}/promote [post]
func promoteUser(service *AdminService) gin.HandlerFunc {
	return mutateUser(service, func(ctx *gin.Context, service *AdminService, id uuid.UUID) (models.User, error) {
		return service.Promote(ctx.Request.Context(), id)
	})
}

func mutateUser(
	service *AdminService,
	mutation func(*gin.Context, *AdminService, uuid.UUID) (models.User, error),
) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		id, err := uuid.Parse(ctx.Param("id"))
		if err != nil {
			writeFailure(ctx, http.StatusBadRequest, "invalid_id", "The id must be a UUID.")
			return
		}
		user, err := mutation(ctx, service, id)
		switch {
		case errors.Is(err, ErrSelfSuspend):
			writeFailure(ctx, http.StatusConflict, "cannot_suspend_self", "An administrator cannot suspend their own account.")
		case errors.Is(err, ErrUserNotFound):
			writeFailure(ctx, http.StatusNotFound, "not_found", "No account has that id.")
		case err != nil:
			slog.Default().ErrorContext(ctx.Request.Context(), "user update failed", "error", err)
			writeFailure(ctx, http.StatusInternalServerError, "user_update_failed", "The account could not be updated. Try again shortly.")
		default:
			ctx.JSON(http.StatusOK, adminUserResponse{User: newAdminUserJSON(user)})
		}
	}
}

func newAdminUserJSON(user models.User) adminUserJSON {
	return adminUserJSON{
		ID:        user.ID,
		Username:  user.Username,
		Role:      user.Role,
		Status:    user.Status,
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
	}
}

func newUserListResponse(page UserPage) userListResponse {
	users := make([]adminUserJSON, 0, len(page.Users))
	for _, user := range page.Users {
		users = append(users, newAdminUserJSON(user))
	}
	response := userListResponse{Users: users}
	if page.NextCursor != nil {
		encoded := paging.EncodeCursor(*page.NextCursor)
		response.NextCursor = &encoded
	}
	return response
}

func optionalFilter(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func parseAdminLimit(value string) (int32, bool) {
	if value == "" {
		return AdminDefaultPageSize, true
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 1 || limit > int(AdminMaximumPageSize) {
		return 0, false
	}
	return int32(limit), true
}

func writeFailure(ctx *gin.Context, status int, code, message string) {
	ctx.JSON(status, openapi.Error{Error: openapi.ErrorDetail{Code: code, Message: message}})
}
