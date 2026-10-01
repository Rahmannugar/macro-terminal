package normalization

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestArticlesNormalizesFeedItems(t *testing.T) {
	sourceID := uuid.New()
	published := time.Date(2026, 10, 1, 12, 30, 0, 0, time.UTC)

	candidates, stats := Articles(Input{
		SourceID:   sourceID,
		SourceType: "news",
		ConfigType: "rss",
		Items: []FeedItem{{
			Title:     "  Fed keeps\t rates steady  ",
			URL:       "HTTPS://Example.com/story?utm_source=rss&fbclid=abc&id=7#section",
			Summary:   " The Federal Reserve held rates unchanged. ",
			Published: published,
		}},
	})

	if len(candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(candidates))
	}
	got := candidates[0]
	if got.SourceID != sourceID {
		t.Errorf("SourceID = %s, want %s", got.SourceID, sourceID)
	}
	if got.Title != "Fed keeps rates steady" {
		t.Errorf("Title = %q, want collapsed whitespace", got.Title)
	}
	if got.URL != "https://example.com/story?id=7" {
		t.Errorf("URL = %q, want tracking params, fragment, and host casing removed", got.URL)
	}
	if got.Content != "The Federal Reserve held rates unchanged." {
		t.Errorf("Content = %q, want trimmed snippet", got.Content)
	}
	if !got.PublishedAt.Equal(published) {
		t.Errorf("PublishedAt = %v, want %v", got.PublishedAt, published)
	}
	if stats.Candidates != 1 || stats.Duplicates != 0 || stats.Invalid != 0 {
		t.Errorf("stats = %+v, want 1/0/0", stats)
	}
}

func TestArticlesDropsDuplicatesByCanonicalURL(t *testing.T) {
	candidates, stats := Articles(Input{
		SourceType: "news",
		ConfigType: "rss",
		Items: []FeedItem{
			{Title: "Same story", URL: "https://example.com/a?utm_campaign=x"},
			{Title: "Same story again", URL: "https://example.com/a#top"},
		},
	})

	if len(candidates) != 1 {
		t.Fatalf("candidates = %d, want 1 (canonical URLs match)", len(candidates))
	}
	if stats.Duplicates != 1 || stats.Candidates != 1 || stats.Invalid != 0 {
		t.Errorf("stats = %+v, want 1/1/0", stats)
	}
}

func TestArticlesDropsInvalidEntries(t *testing.T) {
	candidates, stats := Articles(Input{
		SourceType: "news",
		ConfigType: "rss",
		Items: []FeedItem{
			{Title: "   ", URL: "https://example.com/a"},
			{Title: "No URL at all", URL: "not a url"},
			{Title: "Fine", URL: "ftp://example.com/file"},
		},
	})

	if len(candidates) != 0 {
		t.Fatalf("candidates = %d, want 0", len(candidates))
	}
	if stats.Invalid != 3 || stats.Candidates != 0 || stats.Duplicates != 0 {
		t.Errorf("stats = %+v, want 0/0/3", stats)
	}
}

func TestArticlesParsesGDELTArtList(t *testing.T) {
	body := []byte(`{"articles":[{"title":"Fed holds rates","url":"https://www.reuters.com/markets/story?id=1","seendate":"20261001T121500Z"}]}`)

	candidates, stats := Articles(Input{
		SourceType: "news",
		ConfigType: "api",
		Body:       body,
	})

	if len(candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(candidates))
	}
	want := time.Date(2026, 10, 1, 12, 15, 0, 0, time.UTC)
	if !candidates[0].PublishedAt.Equal(want) {
		t.Errorf("PublishedAt = %v, want %v", candidates[0].PublishedAt, want)
	}
	if stats.Candidates != 1 {
		t.Errorf("stats = %+v, want one candidate", stats)
	}
}

func TestArticlesResolvesRelativeLinksAgainstBaseURL(t *testing.T) {
	candidates, stats := Articles(Input{
		SourceType: "official",
		ConfigType: "rss",
		BaseURL:    "https://www.eia.gov/rss/press_rss.xml",
		Items: []FeedItem{
			{Title: "EIA releases weekly stock report", URL: "/pressroom/releases/press592.php"},
		},
	})

	if len(candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(candidates))
	}
	if candidates[0].URL != "https://www.eia.gov/pressroom/releases/press592.php" {
		t.Errorf("URL = %q, want resolved against the feed URL", candidates[0].URL)
	}
	if stats.Candidates != 1 || stats.Invalid != 0 {
		t.Errorf("stats = %+v, want 1/0/0", stats)
	}
}

func TestArticlesDropsRelativeLinksWithoutBaseURL(t *testing.T) {
	candidates, stats := Articles(Input{
		SourceType: "official",
		ConfigType: "rss",
		Items:      []FeedItem{{Title: "No base available", URL: "/relative/with/no/base"}},
	})

	if len(candidates) != 0 || stats.Invalid != 1 {
		t.Errorf("candidates = %d stats = %+v, want the baseless relative link dropped", len(candidates), stats)
	}
}

func TestArticlesMarksUnexpectedNewsAPIBodyInvalid(t *testing.T) {
	_, stats := Articles(Input{
		SourceType: "news",
		ConfigType: "api",
		Body:       []byte(`{"unexpected":true}`),
	})

	if stats.Invalid != 1 || stats.Candidates != 0 {
		t.Errorf("stats = %+v, want one invalid", stats)
	}
}

func TestArticlesIgnoresNonArticlePayloads(t *testing.T) {
	candidates, stats := Articles(Input{
		SourceType: "official",
		ConfigType: "api",
		Body:       []byte(`{"observations":[{"value":3.1}]}`),
	})

	if len(candidates) != 0 || stats != (Stats{}) {
		t.Errorf("candidates = %d stats = %+v, want none for statistics payloads", len(candidates), stats)
	}
}

// BenchmarkArticlesLargeFeed sizes the normalize/canonicalize/dedupe path on
// a batch the size of the biggest live source.
func BenchmarkArticlesLargeFeed(b *testing.B) {
	items := make([]FeedItem, 0, 640)
	for i := 0; i < 640; i++ {
		items = append(items, FeedItem{
			Title: fmt.Sprintf("Headline number %d about markets and policy", i),
			URL:   fmt.Sprintf("https://example.com/news/story-%d?utm_source=feed", i),
		})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Articles(Input{Items: items, BaseURL: "https://example.com/news"})
	}
}
