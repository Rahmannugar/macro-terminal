package ingestion

import "fmt"

// Adapter converts a successful provider response body into a Result.
// Request construction (URL, parameters, headers) is shared in the fetcher.
type Adapter interface {
	Kind() string
	Parse(body []byte) (Result, error)
}

// AdapterFor returns the adapter for a configuration type (api | rss | web).
func AdapterFor(kind string) (Adapter, error) {
	switch kind {
	case "rss":
		return rssAdapter{}, nil
	case "api":
		return apiAdapter{}, nil
	case "web":
		return webAdapter{}, nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedAdapter, kind)
	}
}

func acceptFor(kind string) string {
	switch kind {
	case "rss":
		return "application/rss+xml, application/atom+xml, application/xml;q=0.9, text/xml;q=0.8, */*;q=0.1"
	case "api":
		return "application/json"
	case "web":
		return "text/html, application/xhtml+xml;q=0.9, */*;q=0.1"
	default:
		return "*/*"
	}
}
