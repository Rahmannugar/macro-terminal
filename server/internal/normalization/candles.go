package normalization

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Candle is one provider price bar.
type Candle struct {
	Timestamp time.Time
	Open      float64
	High      float64
	Low       float64
	Close     float64
}

// CandleBatch is a provider's bar window after validation.
type CandleBatch struct {
	Candles   []Candle
	Malformed int
}

// Candles parses a provider payload into validated price bars. Rows with
// unparseable numbers or timestamps, or with prices that violate the
// high >= low and positive-price invariants the database enforces, are
// dropped and counted instead of failing the whole window.
func Candles(provider string, body []byte) (CandleBatch, error) {
	switch provider {
	case "biquote":
		return biquoteCandles(body)
	case "binance":
		return binanceCandles(body)
	default:
		return CandleBatch{}, fmt.Errorf("unknown candle provider %q", provider)
	}
}

func biquoteCandles(body []byte) (CandleBatch, error) {
	var payload struct {
		Bars []struct {
			OpenTime string      `json:"openTime"`
			Open     json.Number `json:"open"`
			High     json.Number `json:"high"`
			Low      json.Number `json:"low"`
			Close    json.Number `json:"close"`
		} `json:"bars"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return CandleBatch{}, fmt.Errorf("decode biquote candles: %w", err)
	}
	batch := CandleBatch{Candles: make([]Candle, 0, len(payload.Bars))}
	for _, bar := range payload.Bars {
		timestamp, err := time.Parse(time.RFC3339Nano, bar.OpenTime)
		if err != nil {
			batch.Malformed++
			continue
		}
		candle := Candle{Timestamp: timestamp}
		if !candle.fill(bar.Open.String(), bar.High.String(), bar.Low.String(), bar.Close.String()) {
			batch.Malformed++
			continue
		}
		batch.Candles = append(batch.Candles, candle)
	}
	return batch, nil
}

func binanceCandles(body []byte) (CandleBatch, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var rows [][]any
	if err := decoder.Decode(&rows); err != nil {
		return CandleBatch{}, fmt.Errorf("decode binance candles: %w", err)
	}
	batch := CandleBatch{Candles: make([]Candle, 0, len(rows))}
	for _, row := range rows {
		if len(row) < 5 {
			batch.Malformed++
			continue
		}
		openMillis, ok := row[0].(json.Number)
		if !ok {
			batch.Malformed++
			continue
		}
		openTime, err := strconv.ParseInt(openMillis.String(), 10, 64)
		if err != nil {
			batch.Malformed++
			continue
		}
		prices := make([]string, 0, 4)
		for _, cell := range row[1:5] {
			text, isString := cell.(string)
			if !isString {
				prices = nil
				break
			}
			prices = append(prices, text)
		}
		if len(prices) != 4 {
			batch.Malformed++
			continue
		}
		candle := Candle{Timestamp: time.UnixMilli(openTime).UTC()}
		if !candle.fill(prices[0], prices[1], prices[2], prices[3]) {
			batch.Malformed++
			continue
		}
		batch.Candles = append(batch.Candles, candle)
	}
	return batch, nil
}

// fill parses the four prices of one bar and rejects any bar that would
// violate the table's constraints.
func (candle *Candle) fill(open, high, low, closing string) bool {
	openPrice, openErr := strconv.ParseFloat(strings.TrimSpace(open), 64)
	highPrice, highErr := strconv.ParseFloat(strings.TrimSpace(high), 64)
	lowPrice, lowErr := strconv.ParseFloat(strings.TrimSpace(low), 64)
	closePrice, closeErr := strconv.ParseFloat(strings.TrimSpace(closing), 64)
	if openErr != nil || highErr != nil || lowErr != nil || closeErr != nil {
		return false
	}
	if openPrice <= 0 || highPrice <= 0 || lowPrice <= 0 || closePrice <= 0 {
		return false
	}
	if highPrice < lowPrice ||
		highPrice < openPrice ||
		highPrice < closePrice ||
		lowPrice > openPrice ||
		lowPrice > closePrice {
		return false
	}
	candle.Open = openPrice
	candle.High = highPrice
	candle.Low = lowPrice
	candle.Close = closePrice
	return true
}
