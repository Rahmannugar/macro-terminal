package market

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/common/paging"
	marketmodels "github.com/Rahmannugar/macro-terminal/server/internal/market/models"
	"github.com/Rahmannugar/macro-terminal/server/internal/openapi"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type fakeCandles struct {
	rows []marketmodels.StoredCandle
	next *paging.Cursor
	err  error

	gotPairID    uuid.UUID
	gotTimeframe string
	gotLimit     int32
}

func (fake *fakeCandles) CandlesPage(
	_ context.Context,
	entityPairID uuid.UUID,
	timeframe string,
	_ *paging.Cursor,
	limit int32,
) ([]marketmodels.StoredCandle, *paging.Cursor, error) {
	fake.gotPairID = entityPairID
	fake.gotTimeframe = timeframe
	fake.gotLimit = limit
	if fake.err != nil {
		return nil, nil, fake.err
	}
	return fake.rows, fake.next, nil
}

func candlesRouter(t *testing.T, candles Candles) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterRoutes(router, candles)
	return router
}

func candlesPerform(router *gin.Engine, target string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	return recorder
}

func candlesDecodeError(t *testing.T, recorder *httptest.ResponseRecorder) openapi.Error {
	t.Helper()
	var failure openapi.Error
	if err := json.Unmarshal(recorder.Body.Bytes(), &failure); err != nil {
		t.Fatalf("decode failure: %v", err)
	}
	return failure
}

func TestCandlesRejectsBadParameters(t *testing.T) {
	router := candlesRouter(t, &fakeCandles{})

	recorder := candlesPerform(router, "/api/v1/candles?timeframe=1min")
	if recorder.Code != http.StatusBadRequest || candlesDecodeError(t, recorder).Error.Code != "invalid_entity_pair" {
		t.Errorf("missing pair → status/code = %d/%q, want 400 invalid_entity_pair", recorder.Code, candlesDecodeError(t, recorder).Error.Code)
	}

	recorder = candlesPerform(router, "/api/v1/candles?entityPairId="+uuid.NewString()+"&timeframe=1week")
	if recorder.Code != http.StatusBadRequest || candlesDecodeError(t, recorder).Error.Code != "invalid_timeframe" {
		t.Errorf("bad timeframe → status/code = %d/%q, want 400 invalid_timeframe", recorder.Code, candlesDecodeError(t, recorder).Error.Code)
	}

	recorder = candlesPerform(router, "/api/v1/candles?entityPairId="+uuid.NewString()+"&timeframe=1min&limit=0")
	if recorder.Code != http.StatusBadRequest || candlesDecodeError(t, recorder).Error.Code != "invalid_limit" {
		t.Errorf("limit=0 → status/code = %d/%q, want 400 invalid_limit", recorder.Code, candlesDecodeError(t, recorder).Error.Code)
	}

	recorder = candlesPerform(router, "/api/v1/candles?entityPairId="+uuid.NewString()+"&timeframe=1min&cursor=not-base64!!")
	if recorder.Code != http.StatusBadRequest || candlesDecodeError(t, recorder).Error.Code != "invalid_cursor" {
		t.Errorf("garbage cursor → status/code = %d/%q, want 400 invalid_cursor", recorder.Code, candlesDecodeError(t, recorder).Error.Code)
	}
}

func TestCandlesReturnsPage(t *testing.T) {
	pairID := uuid.New()
	candle := marketmodels.StoredCandle{
		ID:           uuid.New(),
		EntityPairID: pairID,
		Timeframe:    "1min",
		Timestamp:    time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC),
		Open:         1.08512,
		High:         1.0853,
		Low:          1.08501,
		Close:        1.0852,
	}
	fake := &fakeCandles{
		rows: []marketmodels.StoredCandle{candle},
		next: &paging.Cursor{At: candle.Timestamp, ID: candle.ID},
	}
	router := candlesRouter(t, fake)

	recorder := candlesPerform(router, "/api/v1/candles?entityPairId="+pairID.String()+"&timeframe=1min")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	var page candleListResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode body %q: %v", recorder.Body.String(), err)
	}
	if len(page.Candles) != 1 {
		t.Fatalf("candles = %+v, want one candle", page.Candles)
	}
	row := page.Candles[0]
	if row.ID != candle.ID || row.EntityPairID != pairID || row.Timeframe != "1min" || row.Close != 1.0852 {
		t.Errorf("candle = %+v, want the stored candle", row)
	}
	if page.NextCursor == nil {
		t.Error("nextCursor = nil, want the page cursor")
	}
	if fake.gotPairID != pairID || fake.gotTimeframe != "1min" || fake.gotLimit != defaultPageSize {
		t.Errorf("query = pair %s %q limit %d, want %s %q %d", fake.gotPairID, fake.gotTimeframe, fake.gotLimit, pairID, "1min", defaultPageSize)
	}
}

func TestCandlesReportsRepositoryErrors(t *testing.T) {
	router := candlesRouter(t, &fakeCandles{err: errFake})

	recorder := candlesPerform(router, "/api/v1/candles?entityPairId="+uuid.NewString()+"&timeframe=1day")
	if recorder.Code != http.StatusInternalServerError || candlesDecodeError(t, recorder).Error.Code != "internal_error" {
		t.Errorf("error → status/code = %d/%q, want 500 internal_error", recorder.Code, candlesDecodeError(t, recorder).Error.Code)
	}
}

var errFake = errors.New("boom")
