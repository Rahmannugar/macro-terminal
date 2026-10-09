package ingestion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/common/ids"
	marketmodels "github.com/Rahmannugar/macro-terminal/server/internal/market/models"
	"github.com/Rahmannugar/macro-terminal/server/internal/normalization"
	"github.com/Rahmannugar/macro-terminal/server/internal/sources/models"
	"github.com/google/uuid"
)

const (
	// backfillTick is how often the job looks for unfilled candle windows.
	backfillTick = 5 * time.Minute
	// backfillWindowsPerConfiguration caps the windows one configuration
	// fetches per pass, so one wide gap cannot starve the other pairs.
	backfillWindowsPerConfiguration = 2
	// backfillPacingSeconds is the minimum spacing between backfill
	// requests to one host, layered on top of the fetcher's spacing.
	backfillPacingSeconds = 1
	// binanceMaxBars is the most klines one Binance request may return.
	binanceMaxBars = 1000
	// biquoteMinuteBars is how many 1-minute slots one biquote window
	// covers. Minute history is a short rolling buffer, so wide windows
	// cross it in a few passes without ever risking the response ceiling.
	biquoteMinuteBars = 4320
	// biquoteDailyBars is how many daily slots one biquote window covers.
	// A response tops out at 500 bars, so windows must stay below it.
	biquoteDailyBars = 400
	// biquoteLimit is the limit query on windowed biquote requests: the
	// response ceiling, kept above every window so no boundary bar is cut.
	biquoteLimit = 500
)

// BackfillCandleStore is the candle storage and gap bookkeeping the
// backfill job needs; the market repository implements it.
type BackfillCandleStore interface {
	UpsertCandles(context.Context, []marketmodels.PersistCandle) (int64, error)
	CandleGaps(
		context.Context,
		uuid.UUID,
		string,
		time.Time,
		time.Time,
		time.Duration,
	) ([]marketmodels.TimeWindow, error)
	BackfillChecks(
		context.Context,
		uuid.UUID,
		string,
		time.Time,
		time.Time,
	) ([]marketmodels.TimeWindow, error)
	RecordBackfillCheck(context.Context, uuid.UUID, string, marketmodels.TimeWindow) error
}

// BackfillJob walks stored candle history and refetches the windows that
// are missing: the first pass loads the configured depth of history, and
// later passes refill holes the live runner never reaches. A window the
// provider answers with no bars (a closed market, history that predates
// the provider) is recorded once so the next pass moves on instead of
// refetching it forever.
type BackfillJob struct {
	configurations ConfigurationSource
	pairs          PairSource
	candles        BackfillCandleStore
	fetcher        SourceFetcher
	dailyDays      int
	minuteDays     int
	logger         *slog.Logger
	now            func() time.Time
}

func NewBackfillJob(
	configurations ConfigurationSource,
	pairs PairSource,
	candles BackfillCandleStore,
	fetcher SourceFetcher,
	dailyDays int,
	minuteDays int,
	logger *slog.Logger,
) *BackfillJob {
	return &BackfillJob{
		configurations: configurations,
		pairs:          pairs,
		candles:        candles,
		fetcher:        fetcher,
		dailyDays:      dailyDays,
		minuteDays:     minuteDays,
		logger:         logger,
		now:            time.Now,
	}
}

// Run blocks until ctx is canceled. The first pass runs immediately so a
// restart resumes the backfill without waiting a full tick.
func (job *BackfillJob) Run(ctx context.Context) error {
	job.backfill(ctx)
	ticker := time.NewTicker(backfillTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			job.backfill(ctx)
		}
	}
}

type backfillStats struct {
	configurations int
	windows        int
	empty          int
	candles        int64
}

func (job *BackfillJob) backfill(ctx context.Context) {
	configurations, err := job.configurations.ListSourceConfigurationsWithSource(ctx)
	if err != nil {
		if ctx.Err() == nil {
			job.logger.ErrorContext(ctx, "Backfill skipped: configurations unavailable",
				"event", "ingestion.backfill.failed",
				"operation", "ingestion.backfill",
				"error", err,
			)
		}
		return
	}
	pairs, err := job.pairs.ListEntityPairs(ctx)
	if err != nil {
		if ctx.Err() == nil {
			job.logger.ErrorContext(ctx, "Backfill skipped: pair index unavailable",
				"event", "ingestion.backfill.failed",
				"operation", "ingestion.backfill",
				"error", err,
			)
		}
		return
	}
	pairIDs := make(map[string]uuid.UUID, len(pairs))
	for _, pair := range pairs {
		pairIDs[pair.Symbol] = pair.ID
	}

	stats := backfillStats{}
	for _, configuration := range configurations {
		if configuration.SourceType != "candles" {
			continue
		}
		if ctx.Err() != nil {
			return
		}
		stats.configurations++
		job.backfillConfiguration(ctx, configuration, pairIDs, &stats)
	}
	if stats.windows > 0 {
		job.logger.InfoContext(ctx, "Backfill cycle finished",
			"event", "ingestion.backfill.cycle",
			"operation", "ingestion.backfill",
			"configurations", stats.configurations,
			"windows", stats.windows,
			"empty_windows", stats.empty,
			"candles", stats.candles,
		)
	}
}

type candleMetadata struct {
	PairSymbol string `json:"pair_symbol"`
	Timeframe  string `json:"timeframe"`
	Provider   string `json:"provider"`
}

func (job *BackfillJob) backfillConfiguration(
	ctx context.Context,
	configuration models.SourceConfigurationWithSource,
	pairIDs map[string]uuid.UUID,
	stats *backfillStats,
) {
	var document struct {
		Candle *candleMetadata `json:"candle"`
	}
	if err := json.Unmarshal(configuration.Config, &document); err != nil ||
		document.Candle == nil ||
		document.Candle.PairSymbol == "" ||
		document.Candle.Timeframe == "" ||
		document.Candle.Provider == "" {
		job.logger.ErrorContext(ctx, "Backfill configuration invalid",
			"event", "ingestion.backfill.failed",
			"operation", "ingestion.backfill",
			"source", configuration.SourceName,
			"reason", "configuration_invalid",
		)
		return
	}
	metadata := document.Candle
	pairID, ok := pairIDs[metadata.PairSymbol]
	if !ok {
		job.logger.ErrorContext(ctx, "Backfill pair unknown",
			"event", "ingestion.backfill.failed",
			"operation", "ingestion.backfill",
			"source", configuration.SourceName,
			"reason", "pair_unknown",
			"pair_symbol", metadata.PairSymbol,
		)
		return
	}

	var interval, minGap time.Duration
	var days int
	switch metadata.Timeframe {
	case "1min":
		interval = time.Minute
		minGap = time.Minute
		days = job.minuteDays
	case "1day":
		interval = 24 * time.Hour
		minGap = 24 * time.Hour
		days = job.dailyDays
	default:
		return
	}
	var maxBars int
	switch metadata.Provider {
	case "biquote":
		maxBars = biquoteDailyBars
		if metadata.Timeframe == "1min" {
			maxBars = biquoteMinuteBars
		}
	case "binance":
		maxBars = binanceMaxBars
	default:
		return
	}

	now := job.now()
	rangeEnd := now
	rangeStart := now.AddDate(0, 0, -days)
	gaps, err := job.candles.CandleGaps(ctx, pairID, metadata.Timeframe, rangeStart, rangeEnd, minGap)
	if err != nil {
		if ctx.Err() == nil {
			job.logger.ErrorContext(ctx, "Backfill gap lookup failed",
				"event", "ingestion.backfill.failed",
				"operation", "ingestion.backfill",
				"source", configuration.SourceName,
				"reason", "gap_lookup_failed",
				"pair_symbol", metadata.PairSymbol,
				"error", err,
			)
		}
		return
	}
	checks, err := job.candles.BackfillChecks(ctx, pairID, metadata.Timeframe, rangeStart, rangeEnd)
	if err != nil {
		if ctx.Err() == nil {
			job.logger.ErrorContext(ctx, "Backfill check lookup failed",
				"event", "ingestion.backfill.failed",
				"operation", "ingestion.backfill",
				"source", configuration.SourceName,
				"reason", "check_lookup_failed",
				"pair_symbol", metadata.PairSymbol,
				"error", err,
			)
		}
		return
	}
	window, ok := largestUncovered(gaps, checks)
	if !ok {
		return
	}

	for fetched := 0; fetched < backfillWindowsPerConfiguration; fetched++ {
		if ctx.Err() != nil {
			return
		}
		if fetched > 0 {
			if err := sleepContext(ctx, backfillPacingSeconds*time.Second); err != nil {
				return
			}
		}
		fetchTo := window.From.Add(time.Duration(maxBars) * interval)
		if fetchTo.After(window.To) {
			fetchTo = window.To
		}
		filled, empty, ok := job.backfillWindow(ctx, configuration, metadata, pairID, window.From, fetchTo)
		if !ok {
			return
		}
		stats.windows++
		if empty {
			stats.empty++
		}
		stats.candles += filled
		window.From = fetchTo
		if !window.From.Before(window.To) {
			return
		}
	}
}

// backfillWindow fetches and stores one window. ok is false when the
// configuration should stop for this pass: the window could not be built,
// fetched, parsed, or stored.
func (job *BackfillJob) backfillWindow(
	ctx context.Context,
	configuration models.SourceConfigurationWithSource,
	metadata *candleMetadata,
	pairID uuid.UUID,
	from, to time.Time,
) (candles int64, empty bool, ok bool) {
	started := job.now()
	windowed, err := windowedConfiguration(configuration, metadata.Provider, from, to)
	if err != nil {
		job.logger.ErrorContext(ctx, "Backfill window configuration failed",
			"event", "ingestion.backfill.failed",
			"operation", "ingestion.backfill",
			"source", configuration.SourceName,
			"reason", "window_configuration_failed",
			"pair_symbol", metadata.PairSymbol,
			"error", err,
		)
		return 0, false, false
	}
	result, err := job.fetcher.Fetch(ctx, windowed)
	if err != nil {
		switch {
		case ctx.Err() != nil:
		case errors.Is(err, ErrCircuitOpen), errors.Is(err, ErrSecretMissing):
			job.logger.WarnContext(ctx, "Backfill window skipped",
				"event", "ingestion.backfill.skipped",
				"operation", "ingestion.backfill",
				"source", configuration.SourceName,
				"pair_symbol", metadata.PairSymbol,
				"circuit_open", errors.Is(err, ErrCircuitOpen),
				"missing_secret", errors.Is(err, ErrSecretMissing),
			)
		default:
			job.logger.ErrorContext(ctx, "Backfill window fetch failed",
				"event", "ingestion.backfill.failed",
				"operation", "ingestion.backfill",
				"source", configuration.SourceName,
				"pair_symbol", metadata.PairSymbol,
				"timeframe", metadata.Timeframe,
				"malformed", errors.Is(err, ErrMalformed),
				"error", err,
			)
		}
		return 0, false, false
	}
	batch, err := normalization.Candles(metadata.Provider, result.Body)
	if err != nil {
		job.logger.ErrorContext(ctx, "Backfill window payload unparsable",
			"event", "ingestion.backfill.failed",
			"operation", "ingestion.backfill",
			"source", configuration.SourceName,
			"reason", "parse_failed",
			"pair_symbol", metadata.PairSymbol,
			"error", err,
		)
		return 0, false, false
	}
	if len(batch.Candles) == 0 {
		if err := job.candles.RecordBackfillCheck(ctx, pairID, metadata.Timeframe, marketmodels.TimeWindow{From: from, To: to}); err != nil {
			if ctx.Err() == nil {
				job.logger.ErrorContext(ctx, "Backfill check store failed",
					"event", "ingestion.backfill.failed",
					"operation", "ingestion.backfill",
					"source", configuration.SourceName,
					"reason", "check_store_failed",
					"pair_symbol", metadata.PairSymbol,
					"error", err,
				)
			}
			return 0, false, false
		}
		job.logger.InfoContext(ctx, "Backfill window empty",
			"event", "ingestion.backfill.empty",
			"operation", "ingestion.backfill",
			"source", configuration.SourceName,
			"pair_symbol", metadata.PairSymbol,
			"timeframe", metadata.Timeframe,
			"window_from", from,
			"window_to", to,
		)
		return 0, true, true
	}

	entries := make([]marketmodels.PersistCandle, 0, len(batch.Candles))
	for _, candle := range batch.Candles {
		id, err := ids.New()
		if err != nil {
			job.logger.ErrorContext(ctx, "Backfill candle id generation failed",
				"event", "ingestion.backfill.failed",
				"operation", "ingestion.backfill",
				"source", configuration.SourceName,
				"reason", "id_failed",
				"pair_symbol", metadata.PairSymbol,
				"error", err,
			)
			return 0, false, false
		}
		entries = append(entries, marketmodels.PersistCandle{
			ID:           id,
			SourceID:     configuration.SourceID,
			EntityPairID: pairID,
			Timeframe:    metadata.Timeframe,
			Timestamp:    candle.Timestamp,
			Open:         candle.Open,
			High:         candle.High,
			Low:          candle.Low,
			Close:        candle.Close,
		})
	}
	upserted, err := job.candles.UpsertCandles(ctx, entries)
	if err != nil {
		if ctx.Err() == nil {
			job.logger.ErrorContext(ctx, "Backfill candle store failed",
				"event", "ingestion.backfill.failed",
				"operation", "ingestion.backfill",
				"source", configuration.SourceName,
				"reason", "store_failed",
				"pair_symbol", metadata.PairSymbol,
				"error", err,
			)
		}
		return 0, false, false
	}
	job.logger.InfoContext(ctx, "Backfilled candles",
		"event", "ingestion.backfill.persisted",
		"operation", "ingestion.backfill",
		"source", configuration.SourceName,
		"pair_symbol", metadata.PairSymbol,
		"timeframe", metadata.Timeframe,
		"provider", metadata.Provider,
		"candles", upserted,
		"malformed", batch.Malformed,
		"window_from", from,
		"window_to", to,
		"attempts", result.Attempts,
		"status_code", result.StatusCode,
		"duration_ms", job.now().Sub(started).Milliseconds(),
	)
	return upserted, false, true
}

// windowedConfiguration rewrites one candle configuration into a provider
// request for a fixed time window: biquote takes from/to in RFC 3339 plus
// a limit, Binance takes startTime/endTime in milliseconds. The original
// configuration ID is kept so the breaker and host spacing state stay
// shared with the live runner.
func windowedConfiguration(
	configuration models.SourceConfigurationWithSource,
	provider string,
	from, to time.Time,
) (models.SourceConfigurationWithSource, error) {
	var document map[string]any
	if err := json.Unmarshal(configuration.Config, &document); err != nil {
		return models.SourceConfigurationWithSource{}, fmt.Errorf("decode configuration: %w", err)
	}
	rawURL, _ := document["url"].(string)
	target, err := url.Parse(rawURL)
	if err != nil || target.Host == "" {
		return models.SourceConfigurationWithSource{}, fmt.Errorf("%w: %s", ErrInvalidURL, rawURL)
	}
	query := target.Query()
	switch provider {
	case "biquote":
		query.Set("limit", strconv.Itoa(biquoteLimit))
		query.Set("from", from.UTC().Format(time.RFC3339))
		query.Set("to", to.UTC().Format(time.RFC3339))
	case "binance":
		query.Del("limit")
		query.Set("startTime", strconv.FormatInt(from.UnixMilli(), 10))
		query.Set("endTime", strconv.FormatInt(to.UnixMilli(), 10))
		query.Set("limit", strconv.Itoa(binanceMaxBars))
	default:
		return models.SourceConfigurationWithSource{}, fmt.Errorf("unsupported backfill provider %q", provider)
	}
	target.RawQuery = query.Encode()

	windowed := map[string]any{
		"url":            target.String(),
		"min_interval_s": backfillPacingSeconds,
		"candle":         document["candle"],
	}
	if bearer, ok := document["bearer_env"]; ok {
		windowed["bearer_env"] = bearer
	}
	if accept, ok := document["accept"]; ok {
		windowed["accept"] = accept
	}
	payload, err := json.Marshal(windowed)
	if err != nil {
		return models.SourceConfigurationWithSource{}, fmt.Errorf("encode windowed configuration: %w", err)
	}
	return models.SourceConfigurationWithSource{
		SourceConfiguration: models.SourceConfiguration{
			ID:       configuration.ID,
			SourceID: configuration.SourceID,
			Type:     configuration.Type,
			Config:   payload,
		},
		SourceName: configuration.SourceName,
		SourceType: configuration.SourceType,
	}, nil
}

// largestUncovered returns the widest stretch of the largest gap that no
// recorded check covers yet.
func largestUncovered(gaps, checks []marketmodels.TimeWindow) (marketmodels.TimeWindow, bool) {
	var best marketmodels.TimeWindow
	found := false
	for _, gap := range gaps {
		segments := []marketmodels.TimeWindow{gap}
		for _, check := range checks {
			segments = subtractCheck(segments, check)
			if len(segments) == 0 {
				break
			}
		}
		for _, segment := range segments {
			if !found || segment.To.Sub(segment.From) > best.To.Sub(best.From) {
				best = segment
				found = true
			}
		}
	}
	return best, found
}

func subtractCheck(
	segments []marketmodels.TimeWindow,
	check marketmodels.TimeWindow,
) []marketmodels.TimeWindow {
	result := make([]marketmodels.TimeWindow, 0, len(segments)+1)
	for _, segment := range segments {
		if !check.To.After(segment.From) || !check.From.Before(segment.To) {
			result = append(result, segment)
			continue
		}
		if check.From.After(segment.From) {
			result = append(result, marketmodels.TimeWindow{From: segment.From, To: check.From})
		}
		if check.To.Before(segment.To) {
			result = append(result, marketmodels.TimeWindow{From: check.To, To: segment.To})
		}
	}
	return result
}
