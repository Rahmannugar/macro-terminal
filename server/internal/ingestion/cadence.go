package ingestion

import "time"

// Cadences is how often each kind of source is fetched.
type Cadences struct {
	News        time.Duration
	Calendar    time.Duration
	Official    time.Duration
	CentralBank time.Duration
	Candles     time.Duration
	Default     time.Duration
}

// DefaultCadences is the standard schedule: news 5m, central banks 15m,
// calendar and official data 30m, candles 1m.
func DefaultCadences() Cadences {
	return Cadences{
		News:        5 * time.Minute,
		Calendar:    30 * time.Minute,
		Official:    30 * time.Minute,
		CentralBank: 15 * time.Minute,
		Candles:     time.Minute,
		Default:     30 * time.Minute,
	}
}

// centralBankSources are polled every 15 minutes instead of the 30-minute
// official-data cadence — central bank news goes stale fast.
var centralBankSources = map[string]bool{
	"Federal Reserve":               true,
	"Bank of England":               true,
	"ECB":                           true,
	"Bank of Canada":                true,
	"Bank of Japan":                 true,
	"Reserve Bank of Australia":     true,
	"Reserve Bank of New Zealand":   true,
	"Swiss National Bank":           true,
	"People's Bank of China (PBOC)": true,
}

// For returns how often one source is fetched: a named central bank gets
// the central-bank cadence, then the source type decides, and everything
// else gets Default.
func (cadences Cadences) For(sourceName, sourceType string) time.Duration {
	if centralBankSources[sourceName] {
		return cadences.CentralBank
	}
	switch sourceType {
	case "news":
		return cadences.News
	case "calendar":
		return cadences.Calendar
	case "official":
		return cadences.Official
	default:
		return cadences.Default
	}
}
