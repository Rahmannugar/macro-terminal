// Package assets serves the signed-in user's watch list and the pair
// catalog the watch-list screen picks from.
package assets

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/authentication"
	"github.com/Rahmannugar/macro-terminal/server/internal/common/paging"
	entitymodels "github.com/Rahmannugar/macro-terminal/server/internal/entities/models"
	entityservices "github.com/Rahmannugar/macro-terminal/server/internal/entities/services"
	"github.com/Rahmannugar/macro-terminal/server/internal/openapi"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	defaultPageSize = 25
	maximumPageSize = 100
)

type WatchList interface {
	PairsForUserPage(
		ctx context.Context,
		userID uuid.UUID,
		cursor *paging.Cursor,
		limit int32,
	) ([]entitymodels.EntityPair, *paging.Cursor, error)
	Subscribe(ctx context.Context, userID, entityPairID uuid.UUID) error
	Unsubscribe(ctx context.Context, userID, entityPairID uuid.UUID) error
}

type Pairs interface {
	ListEntityPairsPage(
		ctx context.Context,
		cursor *paging.Cursor,
		limit int32,
	) ([]entitymodels.EntityPair, *paging.Cursor, error)
}

// RegisterWatchListRoutes mounts the watch-list routes. The caller mounts
// the session guard on the group.
func RegisterWatchListRoutes(router gin.IRoutes, watchList WatchList) {
	router.GET("/assets", listWatchList(watchList))
	router.POST("/assets", followPair(watchList))
	router.DELETE("/assets/:entityPairId", unfollowPair(watchList))
}

// RegisterCatalogRoutes mounts the public pair catalog at /api/v1/entity-pairs.
func RegisterCatalogRoutes(router gin.IRoutes, pairs Pairs) {
	router.GET("/api/v1/entity-pairs", listCatalog(pairs))
}

type entityPairJSON struct {
	ID            uuid.UUID `json:"id" example:"c1e0f4d2-6a1b-4f3e-9d7c-2b8a5e6f4c10"`
	Symbol        string    `json:"symbol" example:"EURUSD"`
	BaseEntityID  uuid.UUID `json:"baseEntityId" example:"9d1e4a63-8f6d-4b9c-1f4e-7a553f8b2a60"`
	QuoteEntityID uuid.UUID `json:"quoteEntityId" example:"3f2a9c81-5b7d-4e2a-8c6f-1d9e4b7a2f30"`
	CreatedAt     time.Time `json:"createdAt" example:"2026-10-06T09:00:00Z"`
	UpdatedAt     time.Time `json:"updatedAt" example:"2026-10-06T09:00:00Z"`
}

type entityPairListResponse struct {
	EntityPairs []entityPairJSON `json:"entityPairs"`
	NextCursor  *string          `json:"nextCursor"`
}

type followRequest struct {
	EntityPairID uuid.UUID `json:"entityPairId" example:"c1e0f4d2-6a1b-4f3e-9d7c-2b8a5e6f4c10"`
}

func newEntityPairJSON(pair entitymodels.EntityPair) entityPairJSON {
	return entityPairJSON{
		ID:            pair.ID,
		Symbol:        pair.Symbol,
		BaseEntityID:  pair.BaseEntityID,
		QuoteEntityID: pair.QuoteEntityID,
		CreatedAt:     pair.CreatedAt,
		UpdatedAt:     pair.UpdatedAt,
	}
}

// @Summary List the watch list.
// @Description Returns the asset pairs the signed-in account follows, newest follows first. Pass the returned nextCursor to fetch the next page.
// @Tags assets
// @Success 200 {object} entityPairListResponse "The followed pairs and the cursor for the next page, if any."
// @Failure 400 {object} openapi.Error "The limit or cursor is invalid."
// @Failure 401 {object} openapi.Error "No valid session exists."
// @Failure 403 {object} openapi.Error "The account is suspended."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/assets [get]
func listWatchList(watchList WatchList) gin.HandlerFunc {
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
		pairs, next, err := watchList.PairsForUserPage(ctx.Request.Context(), account.User.ID, cursor, limit)
		if err != nil {
			writeWatchListFailure(ctx, err)
			return
		}
		ctx.JSON(http.StatusOK, newEntityPairListResponse(pairs, next))
	}
}

// @Summary Follow an asset pair.
// @Description Adds the pair to the signed-in account's watch list. Following the same pair twice keeps a single entry.
// @Tags assets
// @Param body body followRequest true "The pair to follow."
// @Success 204 "The pair is followed."
// @Failure 400 {object} openapi.Error "The request body is missing the pair id."
// @Failure 401 {object} openapi.Error "No valid session exists."
// @Failure 403 {object} openapi.Error "The account is suspended."
// @Failure 404 {object} openapi.Error "The pair does not exist."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/assets [post]
func followPair(watchList WatchList) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		account, ok := authentication.UserAccount(ctx)
		if !ok {
			writeFailure(ctx, http.StatusUnauthorized, "unauthenticated", "Sign in to access your Macro Terminal account.")
			return
		}
		var request followRequest
		if err := ctx.ShouldBindJSON(&request); err != nil {
			writeFailure(ctx, http.StatusBadRequest, "invalid_request", "The request body must be valid JSON.")
			return
		}
		if err := watchList.Subscribe(ctx.Request.Context(), account.User.ID, request.EntityPairID); err != nil {
			writeWatchListFailure(ctx, err)
			return
		}
		ctx.Status(http.StatusNoContent)
	}
}

// @Summary Unfollow an asset pair.
// @Description Removes the pair from the signed-in account's watch list. Removing a pair that is not followed succeeds.
// @Tags assets
// @Param entityPairId path string true "The pair id (UUID)."
// @Success 204 "The pair is not followed."
// @Failure 400 {object} openapi.Error "The pair id is not a UUID."
// @Failure 401 {object} openapi.Error "No valid session exists."
// @Failure 403 {object} openapi.Error "The account is suspended."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/assets/{entityPairId} [delete]
func unfollowPair(watchList WatchList) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		account, ok := authentication.UserAccount(ctx)
		if !ok {
			writeFailure(ctx, http.StatusUnauthorized, "unauthenticated", "Sign in to access your Macro Terminal account.")
			return
		}
		pairID, err := uuid.Parse(ctx.Param("entityPairId"))
		if err != nil {
			writeFailure(ctx, http.StatusBadRequest, "invalid_id", "The pair id must be a UUID.")
			return
		}
		if err := watchList.Unsubscribe(ctx.Request.Context(), account.User.ID, pairID); err != nil {
			writeWatchListFailure(ctx, err)
			return
		}
		ctx.Status(http.StatusNoContent)
	}
}

// @Summary List available asset pairs.
// @Description Returns every tradable pair newest first. Pass the returned nextCursor to fetch the next page.
// @Tags assets
// @Param limit query int false "Page size between 1 and 100. Defaults to 25."
// @Param cursor query string false "Opaque cursor returned as nextCursor by the previous page."
// @Success 200 {object} entityPairListResponse "The pairs and the cursor for the next page, if any."
// @Failure 400 {object} openapi.Error "The limit or cursor is invalid."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/entity-pairs [get]
func listCatalog(pairs Pairs) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		limit, cursor, ok := parsePageQuery(ctx)
		if !ok {
			return
		}
		rows, next, err := pairs.ListEntityPairsPage(ctx.Request.Context(), cursor, limit)
		if err != nil {
			writeFailure(ctx, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
			return
		}
		ctx.JSON(http.StatusOK, newEntityPairListResponse(rows, next))
	}
}

func newEntityPairListResponse(pairs []entitymodels.EntityPair, next *paging.Cursor) entityPairListResponse {
	response := entityPairListResponse{EntityPairs: make([]entityPairJSON, 0, len(pairs))}
	for _, pair := range pairs {
		response.EntityPairs = append(response.EntityPairs, newEntityPairJSON(pair))
	}
	if next != nil {
		encoded := paging.EncodeCursor(*next)
		response.NextCursor = &encoded
	}
	return response
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

func writeWatchListFailure(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, entityservices.ErrEntityPairNotFound):
		writeFailure(ctx, http.StatusNotFound, "entity_pair_not_found", "That asset pair does not exist.")
	case errors.Is(err, entityservices.ErrUserIDRequired), errors.Is(err, entityservices.ErrEntityPairIDRequired):
		writeFailure(ctx, http.StatusBadRequest, "invalid_request", "A pair id is required.")
	default:
		writeFailure(ctx, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
	}
}

func writeFailure(ctx *gin.Context, status int, code, message string) {
	ctx.JSON(status, openapi.Error{Error: openapi.ErrorDetail{Code: code, Message: message}})
}
