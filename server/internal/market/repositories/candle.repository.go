package market

import (
	"context"
	"fmt"
	"strconv"

	"github.com/Rahmannugar/macro-terminal/server/internal/common/paging"
	marketmodels "github.com/Rahmannugar/macro-terminal/server/internal/market/models"
	marketdb "github.com/Rahmannugar/macro-terminal/server/internal/market/repositories/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type CandleRepository struct {
	pool    *pgxpool.Pool
	queries *marketdb.Queries
}

func NewCandleRepository(pool *pgxpool.Pool) *CandleRepository {
	return &CandleRepository{pool: pool, queries: marketdb.New(pool)}
}

// UpsertCandles stores one fetch window atomically; a repeated delivery of
// the same candle rewrites its prices instead of adding a row.
func (repository *CandleRepository) UpsertCandles(
	ctx context.Context,
	candles []marketmodels.PersistCandle,
) (int64, error) {
	if len(candles) == 0 {
		return 0, nil
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin candle upsert: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := repository.queries.WithTx(tx)
	for _, candle := range candles {
		open, err := numericEncode(candle.Open)
		if err != nil {
			return 0, err
		}
		high, err := numericEncode(candle.High)
		if err != nil {
			return 0, err
		}
		low, err := numericEncode(candle.Low)
		if err != nil {
			return 0, err
		}
		closing, err := numericEncode(candle.Close)
		if err != nil {
			return 0, err
		}
		err = queries.UpsertCandle(ctx, marketdb.UpsertCandleParams{
			ID:           candle.ID,
			SourceID:     candle.SourceID,
			EntityPairID: candle.EntityPairID,
			Timeframe:    candle.Timeframe,
			Timestamp:    pgtype.Timestamptz{Time: candle.Timestamp, Valid: true},
			Open:         open,
			High:         high,
			Low:          low,
			Close:        closing,
		})
		if err != nil {
			return 0, fmt.Errorf("upsert candle: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit candle upsert: %w", err)
	}
	return int64(len(candles)), nil
}

func (repository *CandleRepository) CandlesPage(
	ctx context.Context,
	entityPairID uuid.UUID,
	timeframe string,
	cursor *paging.Cursor,
	limit int32,
) ([]marketmodels.StoredCandle, *paging.Cursor, error) {
	params := marketdb.ListCandlesPageParams{
		EntityPairID: entityPairID,
		Timeframe:    timeframe,
		PageSize:     limit + 1,
	}
	if cursor != nil {
		params.CursorTimestamp = pgtype.Timestamptz{Time: cursor.At, Valid: true}
		params.CursorID = pgtype.UUID{Bytes: cursor.ID, Valid: true}
	}
	rows, err := repository.queries.ListCandlesPage(ctx, params)
	if err != nil {
		return nil, nil, fmt.Errorf("list candles page: %w", err)
	}
	candles := make([]marketmodels.StoredCandle, 0, min(len(rows), int(limit)))
	for index, row := range rows {
		if int32(index) == limit {
			last := candles[len(candles)-1]
			return candles, &paging.Cursor{At: last.Timestamp, ID: last.ID}, nil
		}
		open, err := numericDecode(row.Open)
		if err != nil {
			return nil, nil, err
		}
		high, err := numericDecode(row.High)
		if err != nil {
			return nil, nil, err
		}
		low, err := numericDecode(row.Low)
		if err != nil {
			return nil, nil, err
		}
		closing, err := numericDecode(row.Close)
		if err != nil {
			return nil, nil, err
		}
		candles = append(candles, marketmodels.StoredCandle{
			ID:           row.ID,
			EntityPairID: row.EntityPairID,
			Timeframe:    row.Timeframe,
			Timestamp:    row.Timestamp.Time,
			Open:         open,
			High:         high,
			Low:          low,
			Close:        closing,
		})
	}
	return candles, nil, nil
}

func numericEncode(value float64) (pgtype.Numeric, error) {
	var numeric pgtype.Numeric
	text := strconv.FormatFloat(value, 'f', -1, 64)
	if err := numeric.Scan(text); err != nil {
		return pgtype.Numeric{}, fmt.Errorf("encode numeric %q: %w", text, err)
	}
	return numeric, nil
}

func numericDecode(value pgtype.Numeric) (float64, error) {
	if !value.Valid {
		return 0, fmt.Errorf("stored candle price is null")
	}
	raw, err := value.Value()
	if err != nil {
		return 0, fmt.Errorf("decode numeric: %w", err)
	}
	var text string
	switch decoded := raw.(type) {
	case string:
		text = decoded
	case []byte:
		text = string(decoded)
	default:
		return 0, fmt.Errorf("decode numeric: unexpected type %T", raw)
	}
	parsed, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, fmt.Errorf("parse numeric %q: %w", text, err)
	}
	return parsed, nil
}
