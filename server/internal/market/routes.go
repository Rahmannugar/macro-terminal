package market

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/common/paging"
	marketmodels "github.com/Rahmannugar/macro-terminal/server/internal/market/models"
	"github.com/Rahmannugar/macro-terminal/server/internal/openapi"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	defaultPageSize = 25
	maximumPageSize = 100
)

var allowedTimeframes = map[string]bool{"1min": true, "1day": true}

type Candles interface {
	CandlesPage(
		ctx context.Context,
		entityPairID uuid.UUID,
		timeframe string,
		cursor *paging.Cursor,
		limit int32,
	) ([]marketmodels.StoredCandle, *paging.Cursor, error)
}

func RegisterRoutes(router gin.IRoutes, candles Candles) {
	router.GET("/api/v1/candles", listCandles(candles))
}

type candleJSON struct {
	ID           uuid.UUID `json:"id" example:"01993f5c-6b1e-7a4f-9c61-3f0f2f4d8a70"`
	EntityPairID uuid.UUID `json:"entityPairId" example:"01993f5c-6b1e-7a4f-9c61-3f0f2f4d8a71"`
	Timeframe    string    `json:"timeframe" example:"1min"`
	Timestamp    time.Time `json:"timestamp" example:"2026-10-08T12:00:00Z"`
	Open         float64   `json:"open" example:"1.08512"`
	High         float64   `json:"high" example:"1.0853"`
	Low          float64   `json:"low" example:"1.08501"`
	Close        float64   `json:"close" example:"1.0852"`
}

type candleListResponse struct {
	Candles    []candleJSON `json:"candles"`
	NextCursor *string      `json:"nextCursor"`
}

// @Summary List market candles.
// @Description Returns candles for one entity pair and timeframe, newest first. Pass the returned nextCursor to fetch the next page.
// @Tags market
// @Param entityPairId query string true "Entity pair id."
// @Param timeframe query string true "1min or 1day."
// @Param limit query int false "Page size between 1 and 100. Defaults to 25."
// @Param cursor query string false "Opaque cursor returned as nextCursor by the previous page."
// @Success 200 {object} candleListResponse "The candles and the cursor for the next page, if any."
// @Failure 400 {object} openapi.Error "A parameter is invalid."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/candles [get]
func listCandles(candles Candles) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		entityPairID, err := uuid.Parse(ctx.Query("entityPairId"))
		if err != nil {
			writeFailure(ctx, http.StatusBadRequest, "invalid_entity_pair", "entityPairId must be a valid pair id.")
			return
		}
		timeframe := ctx.Query("timeframe")
		if !allowedTimeframes[timeframe] {
			writeFailure(ctx, http.StatusBadRequest, "invalid_timeframe", "timeframe must be 1min or 1day.")
			return
		}
		limit := int32(defaultPageSize)
		if raw := ctx.Query("limit"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 1 || parsed > maximumPageSize {
				writeFailure(ctx, http.StatusBadRequest, "invalid_limit", "Limit must be a number between 1 and 100.")
				return
			}
			limit = int32(parsed)
		}
		cursor, err := paging.DecodeCursor(ctx.Query("cursor"))
		if err != nil {
			writeFailure(ctx, http.StatusBadRequest, "invalid_cursor", "Use the next cursor returned by the previous page.")
			return
		}
		rows, next, err := candles.CandlesPage(ctx.Request.Context(), entityPairID, timeframe, cursor, limit)
		if err != nil {
			writeFailure(ctx, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
			return
		}
		response := candleListResponse{Candles: make([]candleJSON, 0, len(rows))}
		for _, row := range rows {
			response.Candles = append(response.Candles, candleJSON{
				ID:           row.ID,
				EntityPairID: row.EntityPairID,
				Timeframe:    row.Timeframe,
				Timestamp:    row.Timestamp,
				Open:         row.Open,
				High:         row.High,
				Low:          row.Low,
				Close:        row.Close,
			})
		}
		if next != nil {
			encoded := paging.EncodeCursor(*next)
			response.NextCursor = &encoded
		}
		ctx.JSON(http.StatusOK, response)
	}
}

func writeFailure(ctx *gin.Context, status int, code, message string) {
	ctx.JSON(status, openapi.Error{Error: openapi.ErrorDetail{Code: code, Message: message}})
}
