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

// seedUniverse is the source universe: official macro sources, global and
// crypto news, and the economic calendar providers.
var seedUniverse = []seedSource{
	{
		name:       "BLS",
		sourceType: "official",
		configurations: []seedConfiguration{{
			kind: "api",
			// CPI, unemployment, nonfarm payrolls, average hourly
			// earnings, and JOLTS job openings; the registration key is
			// optional and only raises the query limits when set.
			config: `{"url":"https://api.bls.gov/publicAPI/v2/timeseries/data/","seriesid":["CUUR0000SA0","LNS14000000","CES0000000001","CES0500000003","JTS000000000000000JOL"],"registrationkey_env":"MACRO_TERMINAL_BLS_API_KEY"}`,
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
	{
		name:       "U.S. Census Bureau",
		sourceType: "official",
		configurations: []seedConfiguration{{
			kind:   "rss",
			config: `{"url":"https://www.census.gov/content/census/en/newsroom/press-releases.xml"}`,
		}},
	},
	{
		name:       "Federal Reserve",
		sourceType: "official",
		configurations: []seedConfiguration{{
			kind:   "rss",
			config: `{"url":"https://www.federalreserve.gov/feeds/press_all.xml"}`,
		}},
	},
	{
		name:       "ONS",
		sourceType: "official",
		configurations: []seedConfiguration{{
			kind:   "rss",
			config: `{"url":"https://www.ons.gov.uk/releasecalendar?rss="}`,
		}},
	},
	{
		name:       "Bank of England",
		sourceType: "official",
		configurations: []seedConfiguration{{
			kind:   "rss",
			config: `{"url":"https://www.bankofengland.co.uk/rss/statistics"}`,
		}},
	},
	{
		name:       "Eurostat",
		sourceType: "official",
		configurations: []seedConfiguration{{
			kind:   "rss",
			config: `{"url":"https://ec.europa.eu/eurostat/en/search?p_p_id=estatsearchportlet_WAR_estatsearchportlet&p_p_lifecycle=2&p_p_state=maximized&p_p_mode=view&p_p_resource_id=atom&_estatsearchportlet_WAR_estatsearchportlet_collection=CAT_PREREL"}`,
		}},
	},
	{
		name:       "ECB",
		sourceType: "official",
		configurations: []seedConfiguration{{
			kind:   "rss",
			config: `{"url":"https://www.ecb.europa.eu/rss/statpress.html"}`,
		}},
	},
	{
		name:       "Statistics Canada",
		sourceType: "official",
		configurations: []seedConfiguration{{
			kind:   "rss",
			config: `{"url":"https://www150.statcan.gc.ca/n1/rss/dai-quo/0-eng.atom"}`,
		}},
	},
	{
		name:       "Bank of Canada",
		sourceType: "official",
		configurations: []seedConfiguration{{
			kind:   "rss",
			config: `{"url":"https://www.bankofcanada.ca/content_type/press-releases/feed/"}`,
		}},
	},
	{
		name:       "Bank of Japan",
		sourceType: "official",
		configurations: []seedConfiguration{{
			kind:   "rss",
			config: `{"url":"https://www.boj.or.jp/en/rss/whatsnew.xml"}`,
		}},
	},
	{
		name:       "Japanese official statistics",
		sourceType: "official",
		configurations: []seedConfiguration{{
			kind:   "api",
			config: `{"url":"https://api.e-stat.go.jp/rest/3.0/app/json/getStatsList","api_key_param":"appId","api_key_env":"ESTAT_APP_ID"}`,
		}},
	},
	{
		name:       "Australian Bureau of Statistics",
		sourceType: "official",
		configurations: []seedConfiguration{{
			kind:   "api",
			config: `{"url":"https://data.api.abs.gov.au/rest/dataflow/all"}`,
		}},
	},
	{
		name:       "Reserve Bank of Australia",
		sourceType: "official",
		configurations: []seedConfiguration{{
			kind:   "rss",
			config: `{"url":"https://www.rba.gov.au/rss/rss-cb-media-releases.xml"}`,
		}},
	},
	{
		name:       "Stats NZ",
		sourceType: "official",
		configurations: []seedConfiguration{{
			kind:   "api",
			config: `{"url":"https://api.data.stats.govt.nz/rest/dataflow/all/all/latest","accept":"application/xml, application/xml;q=0.9, */*;q=0.1","api_key_param":"subscription-key","api_key_env":"STATSNZ_API_KEY"}`,
		}},
	},
	{
		name:       "Reserve Bank of New Zealand",
		sourceType: "official",
		configurations: []seedConfiguration{{
			kind:   "rss",
			config: `{"url":"https://www.rbnz.govt.nz/feeds/news"}`,
		}},
	},
	{
		name:       "Swiss National Bank",
		sourceType: "official",
		configurations: []seedConfiguration{{
			kind:   "rss",
			config: `{"url":"https://www.snb.ch/public/rss/en/pressrel"}`,
		}},
	},
	{
		name:       "Swiss official statistics",
		sourceType: "official",
		configurations: []seedConfiguration{{
			kind:   "api",
			config: `{"url":"https://disseminate.stats.swiss/rest/dataflow/all?detail=allstubs","accept":"application/xml, application/xml;q=0.9, */*;q=0.1"}`,
		}},
	},
	{
		name:       "National Bureau of Statistics of China (NBS)",
		sourceType: "official",
		configurations: []seedConfiguration{{
			kind:   "rss",
			config: `{"url":"http://www.stats.gov.cn/english/PressRelease/rss.xml"}`,
		}},
	},
	{
		name:       "People's Bank of China (PBOC)",
		sourceType: "official",
		configurations: []seedConfiguration{{
			// The press-release index only lists year archives; articles live
			// on the year page. The URL must be bumped each January.
			kind:   "web",
			config: `{"url":"https://www.pbc.gov.cn/en/3688110/3688172/2026/index.html","selectors":{"item":"ul.prhhul li","title":".ListR a","url":".ListR a","date":".prhhdata","date_layout":"2006-01-02"}}`,
		}},
	},
	{
		name:       "EIA",
		sourceType: "official",
		configurations: []seedConfiguration{
			{
				kind:   "api",
				config: `{"url":"https://api.eia.gov/v2/","api_key_param":"api_key","api_key_env":"MACRO_TERMINAL_EIA_API_KEY"}`,
			},
			{
				kind:   "rss",
				config: `{"url":"https://www.eia.gov/rss/press_rss.xml"}`,
			},
		},
	},
	{name: "OPEC", sourceType: "official"},
	{
		name:       "IEA",
		sourceType: "official",
		configurations: []seedConfiguration{{
			kind:   "web",
			config: `{"url":"https://www.iea.org/news","selectors":{"item":".m-news-detailed-listing__link","title":".m-news-detailed-listing__title","date":".m-news-detailed-listing__date","date_layout":"2 January 2006"}}`,
		}},
	},
	{
		name:       "GDELT",
		sourceType: "news",
		configurations: []seedConfiguration{{
			kind:   "api",
			config: `{"url":"https://api.gdeltproject.org/api/v2/doc/doc","query":"(financial OR economic OR \"central bank\")","language":"english","mode":"ArtList","format":"json","maxrecords":75,"min_interval_s":10}`,
		}},
	},
	{
		name:       "GDELT Crypto",
		sourceType: "news",
		configurations: []seedConfiguration{{
			kind:   "api",
			config: `{"url":"https://api.gdeltproject.org/api/v2/doc/doc","query":"(crypto OR bitcoin OR ethereum)","language":"english","mode":"ArtList","format":"json","maxrecords":75,"min_interval_s":10}`,
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
	{
		name:       "Official crypto/project sources",
		sourceType: "news",
		configurations: []seedConfiguration{
			{kind: "rss", config: `{"url":"https://bitcoincore.org/en/feed.xml"}`},
			{kind: "rss", config: `{"url":"https://blog.ethereum.org/feed.xml"}`},
			{kind: "rss", config: `{"url":"https://github.com/bnb-chain/bsc/releases.atom"}`},
			{kind: "rss", config: `{"url":"https://github.com/anza-xyz/agave/releases.atom"}`},
		},
	},
	{
		name:       "FinanceCalendar",
		sourceType: "calendar",
		configurations: []seedConfiguration{{
			kind:   "api",
			config: `{"url":"https://www.financecalendar.com/wp-json/fc/v1/today"}`,
		}},
	},
	{
		name:       "Biquote",
		sourceType: "calendar",
		configurations: []seedConfiguration{{
			kind:   "api",
			config: `{"url":"https://biquote.io/api/calendar/upcoming"}`,
		}},
	},
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

// Product symbols are authoritative: NQ is stored as NQ, not NQ/USD.
// Non-FX lines are quoted against the dollar.
var seedEntities = []seedEntity{
	// FX majors
	{code: "USD", name: "United States Dollar", kind: "currency"},
	{code: "EUR", name: "Euro", kind: "currency"},
	{code: "GBP", name: "British Pound", kind: "currency"},
	{code: "JPY", name: "Japanese Yen", kind: "currency"},
	{code: "CHF", name: "Swiss Franc", kind: "currency"},
	{code: "CAD", name: "Canadian Dollar", kind: "currency"},
	{code: "AUD", name: "Australian Dollar", kind: "currency"},
	{code: "NZD", name: "New Zealand Dollar", kind: "currency"},
	// Crypto
	{code: "BTC", name: "Bitcoin", kind: "crypto"},
	{code: "ETH", name: "Ethereum", kind: "crypto"},
	{code: "BNB", name: "BNB", kind: "crypto"},
	{code: "SOL", name: "Solana", kind: "crypto"},
	// Commodities
	{code: "XAU", name: "Gold", kind: "commodity"},
	{code: "XAG", name: "Silver", kind: "commodity"},
	{code: "OIL", name: "Crude Oil", kind: "commodity"},
	{code: "COPPER", name: "Copper", kind: "commodity"},
	// Indices
	{code: "DAX", name: "DAX", kind: "index"},
	{code: "ES", name: "S&P 500", kind: "index"},
	{code: "NQ", name: "Nasdaq 100", kind: "index"},
	{code: "FTSE", name: "FTSE 100", kind: "index"},
	{code: "N225", name: "Nikkei 225", kind: "index"},
	{code: "STOXX50", name: "Euro Stoxx 50", kind: "index"},
	// Mapping anchor for China news; fan-out lives in relationship rules.
	{code: "CHINA", name: "China", kind: "country"},
}

var seedPairs = []seedPair{
	// FX majors
	{base: "EUR", quote: "USD", symbol: "EUR/USD"},
	{base: "GBP", quote: "USD", symbol: "GBP/USD"},
	{base: "USD", quote: "JPY", symbol: "USD/JPY"},
	{base: "USD", quote: "CHF", symbol: "USD/CHF"},
	{base: "USD", quote: "CAD", symbol: "USD/CAD"},
	{base: "AUD", quote: "USD", symbol: "AUD/USD"},
	{base: "NZD", quote: "USD", symbol: "NZD/USD"},
	// FX crosses
	{base: "EUR", quote: "GBP", symbol: "EUR/GBP"},
	{base: "EUR", quote: "JPY", symbol: "EUR/JPY"},
	{base: "EUR", quote: "CHF", symbol: "EUR/CHF"},
	{base: "GBP", quote: "JPY", symbol: "GBP/JPY"},
	{base: "AUD", quote: "JPY", symbol: "AUD/JPY"},
	{base: "AUD", quote: "NZD", symbol: "AUD/NZD"},
	{base: "CAD", quote: "JPY", symbol: "CAD/JPY"},
	{base: "NZD", quote: "JPY", symbol: "NZD/JPY"},
	// Crypto
	{base: "BTC", quote: "USD", symbol: "BTC"},
	{base: "ETH", quote: "USD", symbol: "ETH"},
	{base: "BNB", quote: "USD", symbol: "BNB"},
	{base: "SOL", quote: "USD", symbol: "SOL"},
	// Commodities
	{base: "XAU", quote: "USD", symbol: "XAU"},
	{base: "XAG", quote: "USD", symbol: "XAG"},
	{base: "OIL", quote: "USD", symbol: "OIL"},
	{base: "COPPER", quote: "USD", symbol: "COPPER"},
	// Indices
	{base: "DAX", quote: "USD", symbol: "DAX"},
	{base: "ES", quote: "USD", symbol: "ES"},
	{base: "NQ", quote: "USD", symbol: "NQ"},
	{base: "FTSE", quote: "USD", symbol: "FTSE"},
	{base: "N225", quote: "USD", symbol: "N225"},
	{base: "STOXX50", quote: "USD", symbol: "STOXX50"},
}

type seedKnowledgeTerm struct {
	name   string
	kind   string
	entity string
}

// Terms match verbatim, so plural forms sit next to the singulars. Person
// and alias phrases ride the same table as institutions. Officeholders are
// verified as of October 2026: Warsh chairs the Fed (since May 2026),
// Lagarde the ECB, Bailey the BoE, Bullock the RBA, Macklem the BoC,
// Schlegel the SNB, Ueda the BoJ, Bessent the Treasury.
var seedKnowledgeTerms = []seedKnowledgeTerm{
	// Central banks and policy institutions
	{name: "Fed", kind: "institution", entity: "USD"},
	{name: "Federal Reserve", kind: "institution", entity: "USD"},
	{name: "FOMC", kind: "institution", entity: "USD"},
	{name: "Federal Open Market Committee", kind: "institution", entity: "USD"},
	{name: "Fed chair", kind: "institution", entity: "USD"},
	{name: "Fed chairman", kind: "institution", entity: "USD"},
	{name: "Fedspeak", kind: "institution", entity: "USD"},
	{name: "US Treasury", kind: "institution", entity: "USD"},
	{name: "Treasury", kind: "institution", entity: "USD"},
	{name: "ECB", kind: "institution", entity: "EUR"},
	{name: "European Central Bank", kind: "institution", entity: "EUR"},
	{name: "Governing Council", kind: "institution", entity: "EUR"},
	{name: "Bank of England", kind: "institution", entity: "GBP"},
	{name: "BoE", kind: "institution", entity: "GBP"},
	{name: "MPC", kind: "institution", entity: "GBP"},
	{name: "Bank of Japan", kind: "institution", entity: "JPY"},
	{name: "BoJ", kind: "institution", entity: "JPY"},
	{name: "RBA", kind: "institution", entity: "AUD"},
	{name: "Reserve Bank of Australia", kind: "institution", entity: "AUD"},
	{name: "RBNZ", kind: "institution", entity: "NZD"},
	{name: "Reserve Bank of New Zealand", kind: "institution", entity: "NZD"},
	{name: "SNB", kind: "institution", entity: "CHF"},
	{name: "Swiss National Bank", kind: "institution", entity: "CHF"},
	{name: "Bank of Canada", kind: "institution", entity: "CAD"},
	{name: "BoC", kind: "institution", entity: "CAD"},
	{name: "People's Bank of China", kind: "institution", entity: "CHINA"},
	{name: "PBOC", kind: "institution", entity: "CHINA"},

	// Policymakers whose names stand in for their institutions
	{name: "Kevin Warsh", kind: "person", entity: "USD"},
	{name: "Warsh", kind: "person", entity: "USD"},
	{name: "Jerome Powell", kind: "person", entity: "USD"},
	{name: "Powell", kind: "person", entity: "USD"},
	{name: "Scott Bessent", kind: "person", entity: "USD"},
	{name: "Bessent", kind: "person", entity: "USD"},
	{name: "Janet Yellen", kind: "person", entity: "USD"},
	{name: "Yellen", kind: "person", entity: "USD"},
	{name: "Christopher Waller", kind: "person", entity: "USD"},
	{name: "Waller", kind: "person", entity: "USD"},
	{name: "Christine Lagarde", kind: "person", entity: "EUR"},
	{name: "Lagarde", kind: "person", entity: "EUR"},
	{name: "Andrew Bailey", kind: "person", entity: "GBP"},
	{name: "Bailey", kind: "person", entity: "GBP"},
	{name: "Kazuo Ueda", kind: "person", entity: "JPY"},
	{name: "Ueda", kind: "person", entity: "JPY"},
	{name: "Michele Bullock", kind: "person", entity: "AUD"},
	{name: "Bullock", kind: "person", entity: "AUD"},
	{name: "Tiff Macklem", kind: "person", entity: "CAD"},
	{name: "Macklem", kind: "person", entity: "CAD"},
	{name: "Martin Schlegel", kind: "person", entity: "CHF"},
	{name: "Schlegel", kind: "person", entity: "CHF"},

	// Currency prose names
	{name: "dollar", kind: "alias", entity: "USD"},
	{name: "dollars", kind: "alias", entity: "USD"},
	{name: "greenback", kind: "alias", entity: "USD"},
	{name: "greenbacks", kind: "alias", entity: "USD"},
	{name: "buck", kind: "alias", entity: "USD"},
	{name: "bucks", kind: "alias", entity: "USD"},
	{name: "sterling", kind: "alias", entity: "GBP"},
	{name: "pound", kind: "alias", entity: "GBP"},
	{name: "pounds", kind: "alias", entity: "GBP"},
	{name: "cable", kind: "alias", entity: "GBP"},
	{name: "yen", kind: "alias", entity: "JPY"},
	{name: "franc", kind: "alias", entity: "CHF"},
	{name: "francs", kind: "alias", entity: "CHF"},
	{name: "swissy", kind: "alias", entity: "CHF"},
	{name: "loonie", kind: "alias", entity: "CAD"},
	{name: "aussie", kind: "alias", entity: "AUD"},
	{name: "kiwi", kind: "alias", entity: "NZD"},
	{name: "euro", kind: "alias", entity: "EUR"},
	{name: "euros", kind: "alias", entity: "EUR"},
	{name: "fibre", kind: "alias", entity: "EUR"},
	{name: "single currency", kind: "alias", entity: "EUR"},
	{name: "yuan", kind: "alias", entity: "CHINA"},
	{name: "renminbi", kind: "alias", entity: "CHINA"},
	{name: "beijing", kind: "alias", entity: "CHINA"},

	// Market prose names
	{name: "ether", kind: "alias", entity: "ETH"},
	{name: "digital gold", kind: "alias", entity: "BTC"},
	{name: "bullion", kind: "alias", entity: "XAU"},
	{name: "crude", kind: "alias", entity: "OIL"},
	{name: "brent", kind: "alias", entity: "OIL"},
	{name: "wti", kind: "alias", entity: "OIL"},
	{name: "black gold", kind: "alias", entity: "OIL"},
	{name: "gasoline", kind: "alias", entity: "OIL"},
	{name: "Nasdaq", kind: "alias", entity: "NQ"},
	{name: "Nikkei", kind: "alias", entity: "N225"},
	{name: "footsie", kind: "alias", entity: "FTSE"},
	{name: "Stoxx 50", kind: "alias", entity: "STOXX50"},
	{name: "Euro Stoxx", kind: "alias", entity: "STOXX50"},
	{name: "S&P", kind: "alias", entity: "ES"},
	{name: "Wall Street", kind: "alias", entity: "ES"},

	// Policy topics
	{name: "rate cut", kind: "topic", entity: "USD"},
	{name: "rate cuts", kind: "topic", entity: "USD"},
	{name: "rate hike", kind: "topic", entity: "USD"},
	{name: "rate hikes", kind: "topic", entity: "USD"},
	{name: "inflation", kind: "topic", entity: "USD"},
	{name: "CPI", kind: "topic", entity: "USD"},
	{name: "PCE", kind: "topic", entity: "USD"},
	{name: "nonfarm payrolls", kind: "topic", entity: "USD"},
	{name: "non-farm payrolls", kind: "topic", entity: "USD"},
	{name: "payrolls", kind: "topic", entity: "USD"},
	{name: "NFP", kind: "topic", entity: "USD"},
	{name: "jobs report", kind: "topic", entity: "USD"},
	{name: "jobs reports", kind: "topic", entity: "USD"},
	{name: "jobless claims", kind: "topic", entity: "USD"},
	{name: "retail sales", kind: "topic", entity: "USD"},
	{name: "ISM", kind: "topic", entity: "USD"},
	{name: "dot plot", kind: "topic", entity: "USD"},
	{name: "dot plots", kind: "topic", entity: "USD"},
	{name: "hawkish", kind: "topic", entity: "USD"},
	{name: "dovish", kind: "topic", entity: "USD"},
	{name: "soft landing", kind: "topic", entity: "USD"},
	{name: "soft landings", kind: "topic", entity: "USD"},
	{name: "hard landing", kind: "topic", entity: "USD"},
	{name: "hard landings", kind: "topic", entity: "USD"},
	{name: "recession", kind: "topic", entity: "USD"},
	{name: "recessions", kind: "topic", entity: "USD"},
	{name: "taper", kind: "topic", entity: "USD"},
	{name: "tapering", kind: "topic", entity: "USD"},
	{name: "QT", kind: "topic", entity: "USD"},
	{name: "quantitative tightening", kind: "topic", entity: "USD"},
	{name: "carry trade", kind: "topic", entity: "JPY"},
	{name: "carry trades", kind: "topic", entity: "JPY"},
	{name: "tariff", kind: "topic", entity: "CHINA"},
	{name: "tariffs", kind: "topic", entity: "CHINA"},
	{name: "tariff war", kind: "topic", entity: "CHINA"},
	{name: "tariff wars", kind: "topic", entity: "CHINA"},
	{name: "trade war", kind: "topic", entity: "CHINA"},
	{name: "trade wars", kind: "topic", entity: "CHINA"},
}
