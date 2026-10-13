package market

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/common/paging"
	marketmodels "github.com/Rahmannugar/macro-terminal/server/internal/market/models"
	marketdb "github.com/Rahmannugar/macro-terminal/server/internal/market/repositories/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

const maxScanLimit = int32(200000)

// candleAggregation describes how a requested timeframe is built from a
// stored base timeframe: base candles are scanned over a bounded window,
// grouped into aligned buckets, and each bucket collapses to one OHLC row.
type candleAggregation struct {
	baseTimeframe string
	baseSeconds   int64
	bucketSeconds int64
	bucketStart   func(time.Time) time.Time
	bucketEnd     func(time.Time) time.Time
}

var candleAggregations = map[string]candleAggregation{
	"5min": {
		baseTimeframe: "1min",
		baseSeconds:   60,
		bucketSeconds: 300,
		bucketStart:   epochBucket(300),
		bucketEnd:     epochBucketEnd(300),
	},
	"15min": {
		baseTimeframe: "1min",
		baseSeconds:   60,
		bucketSeconds: 900,
		bucketStart:   epochBucket(900),
		bucketEnd:     epochBucketEnd(900),
	},
	"1h": {
		baseTimeframe: "1min",
		baseSeconds:   60,
		bucketSeconds: 3600,
		bucketStart:   epochBucket(3600),
		bucketEnd:     epochBucketEnd(3600),
	},
	"4h": {
		baseTimeframe: "1min",
		baseSeconds:   60,
		bucketSeconds: 14400,
		bucketStart:   epochBucket(14400),
		bucketEnd:     epochBucketEnd(14400),
	},
	"1week": {
		baseTimeframe: "1day",
		baseSeconds:   86400,
		bucketSeconds: 7 * 86400,
		bucketStart:   bucketWeek,
		bucketEnd: func(t time.Time) time.Time {
			return bucketWeek(t).AddDate(0, 0, 7)
		},
	},
	"1month": {
		baseTimeframe: "1day",
		baseSeconds:   86400,
		bucketSeconds: 30 * 86400,
		bucketStart:   bucketMonth,
		bucketEnd: func(t time.Time) time.Time {
			return bucketMonth(t).AddDate(0, 1, 0)
		},
	},
}

// epochBucket aligns timestamps to fixed-size UTC buckets counted from the
// Unix epoch, matching PostgreSQL date_bin over the same interval.
func epochBucket(seconds int64) func(time.Time) time.Time {
	return func(t time.Time) time.Time {
		return time.Unix(t.Unix()/seconds*seconds, 0).UTC()
	}
}

func epochBucketEnd(seconds int64) func(time.Time) time.Time {
	step := time.Duration(seconds) * time.Second
	return func(t time.Time) time.Time {
		return epochBucket(seconds)(t).Add(step)
	}
}

// bucketWeek truncates to the ISO week start (Monday, 00:00 UTC).
func bucketWeek(t time.Time) time.Time {
	utc := t.UTC()
	day := time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
	weekday := (int(day.Weekday()) + 6) % 7
	return day.AddDate(0, 0, -weekday)
}

// bucketMonth truncates to the first day of the month, 00:00 UTC.
func bucketMonth(t time.Time) time.Time {
	utc := t.UTC()
	return time.Date(utc.Year(), utc.Month(), 1, 0, 0, 0, 0, time.UTC)
}

type candleSample struct {
	timestamp time.Time
	open      float64
	high      float64
	low       float64
	close     float64
}

// collapseBuckets groups newest-first base candles into aligned buckets.
// The first sample seen per bucket carries its close; every later (older)
// sample overwrites the open, and the high/low span all samples.
func collapseBuckets(
	samples []candleSample,
	aggregation candleAggregation,
	timeframe string,
	entityPairID uuid.UUID,
) map[time.Time]*marketmodels.StoredCandle {
	buckets := make(map[time.Time]*marketmodels.StoredCandle, len(samples))
	for _, sample := range samples {
		start := aggregation.bucketStart(sample.timestamp)
		existing, ok := buckets[start]
		if !ok {
			buckets[start] = &marketmodels.StoredCandle{
				ID:           uuid.Nil,
				EntityPairID: entityPairID,
				Timeframe:    timeframe,
				Timestamp:    start,
				Open:         sample.open,
				High:         sample.high,
				Low:          sample.low,
				Close:        sample.close,
			}
			continue
		}
		existing.Open = sample.open
		if sample.high > existing.High {
			existing.High = sample.high
		}
		if sample.low < existing.Low {
			existing.Low = sample.low
		}
	}
	return buckets
}

// aggregatedCandlesPage scans base candles over a rolling window, groups
// them into timeframe buckets, and pages over the buckets newest first.
// The scan is bounded so a page never reads the whole history, and the
// cursor walks strictly older than the last returned bucket.
func (repository *CandleRepository) aggregatedCandlesPage(
	ctx context.Context,
	entityPairID uuid.UUID,
	timeframe string,
	aggregation candleAggregation,
	cursor *paging.Cursor,
	limit int32,
) ([]marketmodels.StoredCandle, *paging.Cursor, error) {
	upper := time.Now().UTC()
	if cursor != nil {
		upper = cursor.At
	}
	bucketsPerPage := int64(limit) + 1
	// Align the window floor to a bucket boundary so no bucket straddles it.
	windowStart := aggregation.bucketStart(upper.Add(-time.Duration(bucketsPerPage*2*aggregation.bucketSeconds) * time.Second))
	scanLimit := bucketsPerPage * 2 * aggregation.bucketSeconds / aggregation.baseSeconds
	if scanLimit < int64(limit)*4 {
		scanLimit = int64(limit) * 4
	}
	if scanLimit > int64(maxScanLimit) {
		scanLimit = int64(maxScanLimit)
	}

	rows, err := repository.queries.ListCandlesWindow(ctx, marketdb.ListCandlesWindowParams{
		EntityPairID:    entityPairID,
		Timeframe:       aggregation.baseTimeframe,
		CursorTimestamp: pgtype.Timestamptz{Time: upper, Valid: cursor != nil},
		WindowStart:     pgtype.Timestamptz{Time: windowStart, Valid: true},
		ScanLimit:       int32(scanLimit),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("list candles window: %w", err)
	}
	if len(rows) == 0 {
		return []marketmodels.StoredCandle{}, nil, nil
	}

	samples := make([]candleSample, 0, len(rows))
	for _, row := range rows {
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
		samples = append(samples, candleSample{
			timestamp: row.Timestamp.Time.UTC(),
			open:      open,
			high:      high,
			low:       low,
			close:     closing,
		})
	}
	buckets := collapseBuckets(samples, aggregation, timeframe, entityPairID)

	// A truncated scan leaves the bucket holding the oldest scanned row
	// incomplete; drop it and resume after it so the next page rebuilds it
	// from its remaining candles.
	truncated := int32(len(rows)) >= int32(scanLimit)
	var resumeAfter time.Time
	if truncated {
		oldest := rows[len(rows)-1].Timestamp.Time.UTC()
		partial := aggregation.bucketStart(oldest)
		delete(buckets, partial)
		resumeAfter = aggregation.bucketEnd(partial)
	}

	starts := make([]time.Time, 0, len(buckets))
	for start := range buckets {
		starts = append(starts, start)
	}
	sort.Slice(starts, func(i, j int) bool { return starts[i].After(starts[j]) })

	var next *paging.Cursor
	switch {
	case int32(len(starts)) > limit:
		starts = starts[:limit]
		next = &paging.Cursor{At: starts[len(starts)-1], ID: uuid.Nil}
	case truncated:
		next = &paging.Cursor{At: resumeAfter, ID: uuid.Nil}
	default:
		// The window is fully scanned; older candles may live below it.
		next = &paging.Cursor{At: windowStart, ID: uuid.Nil}
	}

	candles := make([]marketmodels.StoredCandle, 0, len(starts))
	for _, start := range starts {
		candles = append(candles, *buckets[start])
	}
	return candles, next, nil
}
