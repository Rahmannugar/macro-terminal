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
	oandaPracticeBase = "https://api-fxpractice.oanda.com"
	binanceVisionBase = "https://data-api.binance.vision"
)

type candlePair struct {
	symbol     string
	instrument string
	binance    string
}

type candleTimeframe struct {
	label        string
	oandaGran    string
	oandaCount   string
	binanceInt   string
	binanceLimit string
}

var candleTimeframes = []candleTimeframe{
	{label: "1min", oandaGran: "M1", oandaCount: "6", binanceInt: "1m", binanceLimit: "6"},
	{label: "1day", oandaGran: "D", oandaCount: "3", binanceInt: "1d", binanceLimit: "3"},
}

var candlePairs = []candlePair{
	{symbol: "EUR/USD", instrument: "EUR_USD"},
	{symbol: "GBP/USD", instrument: "GBP_USD"},
	{symbol: "USD/JPY", instrument: "USD_JPY"},
	{symbol: "USD/CHF", instrument: "USD_CHF"},
	{symbol: "USD/CAD", instrument: "USD_CAD"},
	{symbol: "AUD/USD", instrument: "AUD_USD"},
	{symbol: "NZD/USD", instrument: "NZD_USD"},
	{symbol: "EUR/GBP", instrument: "EUR_GBP"},
	{symbol: "EUR/JPY", instrument: "EUR_JPY"},
	{symbol: "EUR/CHF", instrument: "EUR_CHF"},
	{symbol: "GBP/JPY", instrument: "GBP_JPY"},
	{symbol: "AUD/JPY", instrument: "AUD_JPY"},
	{symbol: "AUD/NZD", instrument: "AUD_NZD"},
	{symbol: "CAD/JPY", instrument: "CAD_JPY"},
	{symbol: "NZD/JPY", instrument: "NZD_JPY"},
	{symbol: "XAU", instrument: "XAU_USD"},
	{symbol: "XAG", instrument: "XAG_USD"},
	{symbol: "OIL", instrument: "WTICO_USD"},
	{symbol: "COPPER", instrument: "XCU_USD"},
	{symbol: "DAX", instrument: "DE30_EUR"},
	{symbol: "ES", instrument: "SPX500_USD"},
	{symbol: "NQ", instrument: "NAS100_USD"},
	{symbol: "FTSE", instrument: "UK100_GBP"},
	{symbol: "N225", instrument: "JP225_USD"},
	{symbol: "STOXX50", instrument: "EU50_EUR"},
	{symbol: "BTC", binance: "BTCUSDT"},
	{symbol: "ETH", binance: "ETHUSDT"},
	{symbol: "BNB", binance: "BNBUSDT"},
	{symbol: "SOL", binance: "SOLUSDT"},
}

var seedCandleSources = buildCandleSources()

func buildCandleSources() []seedSource {
	var oandaConfigs, binanceConfigs []seedConfiguration
	for _, pair := range candlePairs {
		for _, timeframe := range candleTimeframes {
			if pair.instrument != "" {
				target := fmt.Sprintf(
					"%s/v3/instruments/%s/candles?granularity=%s&count=%s&price=M",
					oandaPracticeBase, pair.instrument, timeframe.oandaGran, timeframe.oandaCount,
				)
				oandaConfigs = append(oandaConfigs, seedConfiguration{
					kind: "api",
					config: fmt.Sprintf(
						`{"url":%q,"bearer_env":"MACRO_TERMINAL_OANDA_TOKEN","candle":{"provider":"oanda","pair_symbol":%q,"timeframe":%q}}`,
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
		{name: "OANDA", sourceType: "candles", configurations: oandaConfigs},
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
