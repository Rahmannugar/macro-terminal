package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Rahmannugar/macro-terminal/server/internal/common/ids"
	sourcemodels "github.com/Rahmannugar/macro-terminal/server/internal/sources/models"
	"github.com/Rahmannugar/macro-terminal/server/internal/sources/repositories"
	"github.com/google/uuid"
)

const (
	biquoteBase       = "https://biquote.io/api"
	binanceVisionBase = "https://data-api.binance.vision"
)

type candlePair struct {
	symbol  string
	biquote string
	binance string
}

type candleTimeframe struct {
	label        string
	biquoteInt   string
	biquoteLimit string
	binanceInt   string
	binanceLimit string
}

var candleTimeframes = []candleTimeframe{
	{label: "1min", biquoteInt: "1m", biquoteLimit: "6", binanceInt: "1m", binanceLimit: "6"},
	{label: "1day", biquoteInt: "1d", biquoteLimit: "3", binanceInt: "1d", binanceLimit: "3"},
}

var candlePairs = []candlePair{
	{symbol: "EUR/USD", biquote: "EURUSD"},
	{symbol: "GBP/USD", biquote: "GBPUSD"},
	{symbol: "USD/JPY", biquote: "USDJPY"},
	{symbol: "USD/CHF", biquote: "USDCHF"},
	{symbol: "USD/CAD", biquote: "USDCAD"},
	{symbol: "AUD/USD", biquote: "AUDUSD"},
	{symbol: "NZD/USD", biquote: "NZDUSD"},
	{symbol: "EUR/GBP", biquote: "EURGBP"},
	{symbol: "EUR/JPY", biquote: "EURJPY"},
	{symbol: "EUR/CHF", biquote: "EURCHF"},
	{symbol: "GBP/JPY", biquote: "GBPJPY"},
	{symbol: "AUD/JPY", biquote: "AUDJPY"},
	{symbol: "AUD/NZD", biquote: "AUDNZD"},
	{symbol: "CAD/JPY", biquote: "CADJPY"},
	{symbol: "NZD/JPY", biquote: "NZDJPY"},
	{symbol: "XAU", biquote: "XAUUSD"},
	{symbol: "XAG", biquote: "XAGUSD"},
	{symbol: "OIL", biquote: "USOIL"},
	{symbol: "COPPER", biquote: "XCUUSD"},
	{symbol: "DAX", biquote: "DE30"},
	{symbol: "ES", biquote: "US500"},
	{symbol: "NQ", biquote: "USTEC"},
	{symbol: "FTSE", biquote: "UK100"},
	{symbol: "N225", biquote: "JP225"},
	{symbol: "STOXX50", biquote: "STOXX50"},
	{symbol: "BTC", binance: "BTCUSDT"},
	{symbol: "ETH", binance: "ETHUSDT"},
	{symbol: "BNB", binance: "BNBUSDT"},
	{symbol: "SOL", binance: "SOLUSDT"},
}

var seedCandleSources = buildCandleSources()

func buildCandleSources() []seedSource {
	var biquoteConfigs, binanceConfigs []seedConfiguration
	for _, pair := range candlePairs {
		for _, timeframe := range candleTimeframes {
			if pair.biquote != "" {
				target := fmt.Sprintf(
					"%s/%s/ohlc?interval=%s&limit=%s",
					biquoteBase, pair.biquote, timeframe.biquoteInt, timeframe.biquoteLimit,
				)
				biquoteConfigs = append(biquoteConfigs, seedConfiguration{
					kind: "api",
					config: fmt.Sprintf(
						`{"url":%q,"candle":{"provider":"biquote","pair_symbol":%q,"timeframe":%q}}`,
						target, pair.symbol, timeframe.label,
					),
				})
			}
			if pair.binance != "" {
				target := fmt.Sprintf(
					"%s/api/v3/klines?symbol=%s&interval=%s&limit=%s",
					binanceVisionBase, pair.binance, timeframe.binanceInt, timeframe.binanceLimit,
				)
				binanceConfigs = append(binanceConfigs, seedConfiguration{
					kind: "api",
					config: fmt.Sprintf(
						`{"url":%q,"candle":{"provider":"binance","pair_symbol":%q,"timeframe":%q}}`,
						target, pair.symbol, timeframe.label,
					),
				})
			}
		}
	}
	return []seedSource{
		{name: "Biquote Candles", sourceType: "candles", configurations: biquoteConfigs},
		{name: "Binance", sourceType: "candles", configurations: binanceConfigs},
	}
}

// ensureCandleConfiguration creates a candle configuration when its URL is
// new. Candle sources hold one configuration per pair and timeframe, so the
// seed creates them by URL instead of the single-configuration ensure flow.
func ensureCandleConfiguration(
	ctx context.Context,
	repository *repositories.SourceRepository,
	sourceID uuid.UUID,
	configuration seedConfiguration,
) error {
	var document map[string]any
	if err := json.Unmarshal([]byte(configuration.config), &document); err != nil {
		return fmt.Errorf("decode candle configuration: %w", err)
	}
	configURL, _ := document["url"].(string)
	if strings.TrimSpace(configURL) == "" {
		return errors.New("candle configuration has no url")
	}
	if _, err := repository.SourceConfigurationByURL(ctx, sourceID, configuration.kind, configURL); err == nil {
		return nil
	} else if !errors.Is(err, sourcemodels.ErrSourceConfigurationNotFound) {
		return err
	}
	id, err := ids.New()
	if err != nil {
		return fmt.Errorf("generate configuration id: %w", err)
	}
	_, err = repository.CreateSourceConfiguration(ctx, sourcemodels.SourceConfiguration{
		ID:       id,
		SourceID: sourceID,
		Type:     configuration.kind,
		Config:   []byte(configuration.config),
	})
	if err != nil {
		return fmt.Errorf("create candle configuration: %w", err)
	}
	return nil
}
