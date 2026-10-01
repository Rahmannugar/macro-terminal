// Package normalization turns fetched provider payloads into canonical
// article candidates. The fetch stage decides what to download; this stage
// decides what the downloaded entries actually are: one clean title, one
// canonical URL, deduplicated within the batch.
//
// Storage happens after mapping, following the pipeline: normalize,
// dedupe, map, store.
package normalization

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

// FeedItem is one entry from an already-parsed feed. The ingestion package
// converts its own Item into this type at the package boundary.
type FeedItem struct {
	Title     string
	URL       string
	Summary   string
	Published time.Time
}

// Input is one fetched result to normalize.
type Input struct {
	SourceID   uuid.UUID
	SourceType string
	ConfigType string // api | rss | web
	Items      []FeedItem
	Body       []byte
	BaseURL    string // request URL — relative links resolve against it
}

// Candidate is one deduplicated article, ready for mapping and storage.
type Candidate struct {
	SourceID    uuid.UUID
	Title       string
	Content     string    // snippet only — full publisher text is a licensing question
	URL         string    // canonical: no tracking parameters, no fragment
	PublishedAt time.Time // zero when the provider supplied no date
}

// Stats records what normalization kept and dropped, for per-source
// observability.
type Stats struct {
	Candidates int // unique, valid candidates kept
	Duplicates int // dropped because their canonical URL already appeared
	Invalid    int // dropped because the title or URL was missing or unusable
}

// Articles normalizes one fetched result. Feed results use their parsed
// items; news API results are parsed as GDELT's ArtList — the only news API
// shape in the source universe. Other payloads (statistics responses, HTML
// pages) yield no article candidates.
func Articles(in Input) ([]Candidate, Stats) {
	var stats Stats

	items := in.Items
	if len(items) == 0 && in.SourceType == "news" && in.ConfigType == "api" {
		parsed, err := gdeltArticles(in.Body)
		if err != nil {
			stats.Invalid++
			return nil, stats
		}
		items = parsed
	}

	seen := make(map[string]bool, len(items))
	candidates := make([]Candidate, 0, len(items))
	for _, item := range items {
		candidate, ok := newCandidate(in, item)
		if !ok {
			stats.Invalid++
			continue
		}
		if seen[candidate.URL] {
			stats.Duplicates++
			continue
		}
		seen[candidate.URL] = true
		candidates = append(candidates, candidate)
	}
	stats.Candidates = len(candidates)
	return candidates, stats
}

func newCandidate(in Input, item FeedItem) (Candidate, bool) {
	canonical, ok := canonicalURL(item.URL, in.BaseURL)
	if !ok {
		return Candidate{}, false
	}
	title := collapseWhitespace(item.Title)
	if title == "" {
		return Candidate{}, false
	}
	return Candidate{
		SourceID:    in.SourceID,
		Title:       title,
		Content:     strings.TrimSpace(item.Summary),
		URL:         canonical,
		PublishedAt: item.Published,
	}, true
}

// gdeltArticles parses GDELT's ArtList JSON.
func gdeltArticles(body []byte) ([]FeedItem, error) {
	var payload struct {
		Articles []struct {
			Title    string `json:"title"`
			URL      string `json:"url"`
			SeenDate string `json:"seendate"`
		} `json:"articles"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parse artlist: %w", err)
	}
	if payload.Articles == nil {
		return nil, fmt.Errorf("parse artlist: response has no articles list")
	}
	items := make([]FeedItem, 0, len(payload.Articles))
	for _, article := range payload.Articles {
		items = append(items, FeedItem{
			Title:     article.Title,
			URL:       article.URL,
			Published: parseSeenDate(article.SeenDate),
		})
	}
	return items, nil
}

// parseSeenDate reads GDELT's seendate format: 20261001T121500Z.
func parseSeenDate(value string) time.Time {
	parsed, err := time.Parse("20060102T150405Z", value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

// trackingParameters are campaign and analytics parameters that do not
// change which article a URL points at.
var trackingParameters = map[string]bool{
	"fbclid":  true,
	"gclid":   true,
	"msclkid": true,
	"igshid":  true,
	"mc_cid":  true,
	"mc_eid":  true,
}

// canonicalURL strips everything that does not change the article: the
// fragment, campaign parameters, and host casing. Relative links (some
// feeds publish them) are resolved against the request URL first. Two URLs
// that canonicalize to the same string are the same article.
func canonicalURL(raw, base string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", false
	}
	if parsed.Host == "" {
		parsed, err = resolveBase(parsed, base)
		if err != nil {
			return "", false
		}
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", false
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Fragment = ""

	query := parsed.Query()
	for key := range query {
		if strings.HasPrefix(key, "utm_") || trackingParameters[key] {
			query.Del(key)
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), true
}

func resolveBase(reference *url.URL, base string) (*url.URL, error) {
	parsedBase, err := url.Parse(base)
	if err != nil || parsedBase.Host == "" {
		return nil, fmt.Errorf("relative link with no base URL")
	}
	return parsedBase.ResolveReference(reference), nil
}

func collapseWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
