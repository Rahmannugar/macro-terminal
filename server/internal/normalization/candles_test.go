package normalization

import (
	"testing"
	"time"
)

func TestCandlesParsesBiquoteBody(t *testing.T) {
	body := []byte(`{
		"symbol": "EURUSD",
		"interval": "1m",
		"bars": [
			{"openTime":"2026-10-08T12:00:00Z","open":1.08512,"high":1.08530,"low":1.08501,"close":1.08520,"volume":0,"tickVolume":412,"isOpen":false},
			{"openTime":"2026-10-08T12:01:00Z","open":1.08520,"high":1.08545,"low":1.08510,"close":1.08540,"volume":0,"tickVolume":388,"isOpen":true},
			{"openTime":"2026-10-08T12:02:00Z","open":1.08540,"high":1.08500,"low":1.08550,"close":1.08545},
			{"openTime":"not-a-time","open":1.08540,"high":1.08550,"low":1.08530,"close":1.08545},
			{"openTime":"2026-10-08T12:04:00Z"}
		]
	}`)

	batch, err := Candles("biquote", body)
	if err != nil {
		t.Fatalf("Candles: %v", err)
	}
	if len(batch.Candles) != 2 {
		t.Fatalf("candles = %d, want 2 (forming bars count, broken rows drop)", len(batch.Candles))
	}
	if batch.Malformed != 3 {
		t.Fatalf("malformed = %d, want 3", batch.Malformed)
	}
	first := batch.Candles[0]
	want := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	if !first.Timestamp.Equal(want) {
		t.Errorf("timestamp = %s, want %s", first.Timestamp, want)
	}
	if first.Open != 1.08512 || first.High != 1.0853 || first.Low != 1.08501 || first.Close != 1.0852 {
		t.Errorf("first candle = %+v, want the parsed prices", first)
	}
}

func TestCandlesParsesBinanceBody(t *testing.T) {
	body := []byte(`[
		[1791456000000,"61000.10","62500.50","60500.25","61800.75",1791456059999,"123.45"],
		[1791456060000,"61800.75","63000.00","61500.00","62000.00",1791456119999,"99.10"],
		[1791456120000,"62000.00","61000.00","62500.00","62200.00",1791456179999,"10.00"],
		[1791456180000,"62200.00"],
		["not-a-number","62000.00","62500.00","61500.00","62200.00"]
	]`)

	batch, err := Candles("binance", body)
	if err != nil {
		t.Fatalf("Candles: %v", err)
	}
	if len(batch.Candles) != 2 {
		t.Fatalf("candles = %d, want 2", len(batch.Candles))
	}
	if batch.Malformed != 3 {
		t.Fatalf("malformed = %d, want 3", batch.Malformed)
	}
	first := batch.Candles[0]
	want := time.UnixMilli(1791456000000).UTC()
	if !first.Timestamp.Equal(want) {
		t.Errorf("timestamp = %s, want %s", first.Timestamp, want)
	}
	if first.Open != 61000.10 || first.High != 62500.50 || first.Low != 60500.25 || first.Close != 61800.75 {
		t.Errorf("first candle = %+v, want the parsed kline prices", first)
	}
}

func TestCandlesRejectsUnknownProvider(t *testing.T) {
	if _, err := Candles("twelvedata", []byte(`{}`)); err == nil {
		t.Fatal("error = nil, want unknown provider error")
	}
}

func TestCandlesRejectsUnusableBodies(t *testing.T) {
	if _, err := Candles("biquote", []byte(`not json`)); err == nil {
		t.Error("biquote: error = nil, want decode failure")
	}
	if _, err := Candles("binance", []byte(`{}`)); err == nil {
		t.Error("binance: error = nil, want decode failure")
	}
}
