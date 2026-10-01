package ingestion

import "time"

// Cadences are the scheduled ingestion cadences expressed as configurable
// constants — tunable per deployment without changing the scheduling
// architecture.
type Cadences struct {
	News        time.Duration
	Calendar    time.Duration
	Official    time.Duration
	CentralBank time.Duration
	Candles     time.Duration
	Default     time.Duration
}

// DefaultCadences returns the standard cadences.
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

// centralBankSources run at the central-bank cadence (15m) instead of the
// official-data cadence (30m): monetary-policy communications need fresher
// polling.
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

// For resolves the cadence for a source. The candles cadence applies to
// market-data feeds.
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
