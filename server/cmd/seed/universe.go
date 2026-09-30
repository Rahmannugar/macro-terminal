package main

type seedConfiguration struct {
	kind   string
	config string
}

type seedSource struct {
	name           string
	sourceType     string
	configurations []seedConfiguration
}

// seedUniverse is the §9 source universe: official macro sources, global and
// crypto news, and the economic calendar providers. Configurations only exist
// where the access endpoint is known; the rest are onboarded (and configured
// by admins) when their adapters arrive. Secrets are never stored here —
// configuration references environment variables through *_env keys.
var seedUniverse = []seedSource{
	{
		name:       "BLS",
		sourceType: "official",
		configurations: []seedConfiguration{{
			kind:   "api",
			config: `{"url":"https://api.bls.gov/publicAPI/v2/timeseries/data/"}`,
		}},
	},
	{
		name:       "BEA",
		sourceType: "official",
		configurations: []seedConfiguration{{
			kind:   "api",
			config: `{"url":"https://apps.bea.gov/api/data","api_key_param":"UserID","api_key_env":"MACRO_TERMINAL_BEA_API_KEY"}`,
		}},
	},
	{name: "U.S. Census Bureau", sourceType: "official"},
	{
		name:       "Federal Reserve",
		sourceType: "official",
		configurations: []seedConfiguration{{
			kind:   "rss",
			config: `{"url":"https://www.federalreserve.gov/feeds/press_all.xml"}`,
		}},
	},
	{name: "ONS", sourceType: "official"},
	{name: "Bank of England", sourceType: "official"},
	{name: "Eurostat", sourceType: "official"},
	{name: "ECB", sourceType: "official"},
	{name: "Statistics Canada", sourceType: "official"},
	{name: "Bank of Canada", sourceType: "official"},
	{name: "Bank of Japan", sourceType: "official"},
	{name: "Japanese official statistics", sourceType: "official"},
	{name: "Australian Bureau of Statistics", sourceType: "official"},
	{name: "Reserve Bank of Australia", sourceType: "official"},
	{name: "Stats NZ", sourceType: "official"},
	{name: "Reserve Bank of New Zealand", sourceType: "official"},
	{name: "Swiss National Bank", sourceType: "official"},
	{name: "Swiss official statistics", sourceType: "official"},
	{name: "National Bureau of Statistics of China (NBS)", sourceType: "official"},
	{name: "People's Bank of China (PBOC)", sourceType: "official"},
	{
		name:       "EIA",
		sourceType: "official",
		configurations: []seedConfiguration{{
			kind:   "api",
			config: `{"url":"https://api.eia.gov/v2/","api_key_param":"api_key","api_key_env":"MACRO_TERMINAL_EIA_API_KEY"}`,
		}},
	},
	{name: "OPEC", sourceType: "official"},
	{
		name:       "GDELT",
		sourceType: "news",
		configurations: []seedConfiguration{{
			kind:   "api",
			config: `{"url":"https://api.gdeltproject.org/api/v2/doc/doc","query":"(financial OR economic OR \"central bank\")","language":"english"}`,
		}},
	},
	{
		name:       "GDELT Crypto",
		sourceType: "news",
		configurations: []seedConfiguration{{
			kind:   "api",
			config: `{"url":"https://api.gdeltproject.org/api/v2/doc/doc","query":"(crypto OR bitcoin OR ethereum)","language":"english"}`,
		}},
	},
	{
		name:       "CoinDesk",
		sourceType: "news",
		configurations: []seedConfiguration{{
			kind:   "rss",
			config: `{"url":"https://www.coindesk.com/arc/outboundfeeds/rss/"}`,
		}},
	},
	{
		name:       "Cointelegraph",
		sourceType: "news",
		configurations: []seedConfiguration{{
			kind:   "rss",
			config: `{"url":"https://cointelegraph.com/rss"}`,
		}},
	},
	{name: "Official crypto/project sources", sourceType: "news"},
	{name: "FinanceCalendar", sourceType: "calendar"},
	{name: "Biquote", sourceType: "calendar"},
}

type seedEntity struct {
	code string
	name string
	kind string
}

type seedPair struct {
	base   string
	quote  string
	symbol string
}

// seedGraph is the §7.2 example asset universe: base/quote entities and the
// authoritative product symbols (NQ stays NQ, not NQ/USD). Equities, crypto,
// commodities, and gold are all quoted against the dollar. Admins can adjust
// any of this later through the dashboard.
var seedEntities = []seedEntity{
	{code: "USD", name: "United States Dollar", kind: "currency"},
	{code: "GBP", name: "British Pound", kind: "currency"},
	{code: "EUR", name: "Euro", kind: "currency"},
	{code: "CHF", name: "Swiss Franc", kind: "currency"},
	{code: "JPY", name: "Japanese Yen", kind: "currency"},
	{code: "XAU", name: "Gold", kind: "commodity"},
	{code: "DAX", name: "DAX", kind: "index"},
	{code: "ES", name: "S&P 500", kind: "index"},
	{code: "NQ", name: "Nasdaq 100", kind: "index"},
	{code: "OIL", name: "Crude Oil", kind: "commodity"},
	{code: "BTC", name: "Bitcoin", kind: "crypto"},
}

var seedPairs = []seedPair{
	{base: "GBP", quote: "USD", symbol: "GBP/USD"},
	{base: "EUR", quote: "USD", symbol: "EUR/USD"},
	{base: "USD", quote: "CHF", symbol: "USD/CHF"},
	{base: "USD", quote: "JPY", symbol: "USD/JPY"},
	{base: "XAU", quote: "USD", symbol: "XAU"},
	{base: "DAX", quote: "USD", symbol: "DAX"},
	{base: "ES", quote: "USD", symbol: "ES"},
	{base: "NQ", quote: "USD", symbol: "NQ"},
	{base: "OIL", quote: "USD", symbol: "OIL"},
	{base: "BTC", quote: "USD", symbol: "BTC"},
}
