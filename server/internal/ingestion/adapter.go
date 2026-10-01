package ingestion

import "fmt"

// Adapter turns a fetched response body into a Result. Building the request
// itself (URL, query, headers) is the fetcher's job — adapters only parse.
type Adapter interface {
	Kind() string
	Parse(body []byte) (Result, error)
}

// AdapterFor returns the adapter for a configuration type: "api", "rss",
// or "web".
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

// acceptFor is the default Accept header per type; a configuration's
// "accept" key overrides it.
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
