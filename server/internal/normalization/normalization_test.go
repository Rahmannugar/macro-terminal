package normalization

import (
	"fmt"
	"strings"
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

func TestArticlesPrefersFullContentOverSummary(t *testing.T) {
	candidates, _ := Articles(Input{
		SourceID:   uuid.New(),
		SourceType: "news",
		ConfigType: "rss",
		Items: []FeedItem{{
			Title:   "Fed keeps rates steady",
			URL:     "https://example.com/story",
			Content: "  <p>The full article body the feed shipped.</p> ",
			Summary: "Short teaser.",
		}},
	})

	if len(candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(candidates))
	}
	if got := candidates[0].Content; got != "<p>The full article body the feed shipped.</p>" {
		t.Errorf("Content = %q, want the feed's full content", got)
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
	body := []byte(`{"articles":[{"title":"Fed holds rates","url":"https://www.reuters.com/markets/story?id=1","seendate":"20261001T121500Z","socialimage":"https://cdn.reuters.com/photos/policy.jpg"}]}`)

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
	if candidates[0].ImageURL != "https://cdn.reuters.com/photos/policy.jpg" {
		t.Errorf("ImageURL = %q, want the social image", candidates[0].ImageURL)
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

func TestArticlesNormalizesImageURL(t *testing.T) {
	candidates, _ := Articles(Input{
		SourceType: "news",
		ConfigType: "rss",
		BaseURL:    "https://example.com/releases",
		Items: []FeedItem{
			{Title: "Absolute", URL: "https://example.com/a", ImageURL: "HTTPS://CDN.Example.com/photo.png?width=800#crop"},
			{Title: "Relative", URL: "https://example.com/b", ImageURL: "/photos/b.jpg"},
			{Title: "Script", URL: "https://example.com/c", ImageURL: "javascript:alert(1)"},
			{Title: "Missing", URL: "https://example.com/d"},
			{Title: "Too long", URL: "https://example.com/e", ImageURL: "https://cdn.example.com/" + strings.Repeat("a", 2100)},
		},
	})

	got := map[string]string{}
	for _, candidate := range candidates {
		got[candidate.Title] = candidate.ImageURL
	}
	if len(candidates) != 5 {
		t.Fatalf("candidates = %d, want 5 (a rejected image drops the image, not the article)", len(candidates))
	}
	if got["Absolute"] != "https://cdn.example.com/photo.png?width=800" {
		t.Errorf("Absolute ImageURL = %q, want scheme and host lowercased, query kept, fragment dropped", got["Absolute"])
	}
	if got["Relative"] != "https://example.com/photos/b.jpg" {
		t.Errorf("Relative ImageURL = %q, want resolved against the request URL", got["Relative"])
	}
	for _, title := range []string{"Script", "Missing", "Too long"} {
		if got[title] != "" {
			t.Errorf("%s ImageURL = %q, want empty", title, got[title])
		}
	}
}

func TestArticlesStripsMarkupAndEntitiesFromTitle(t *testing.T) {
	candidates, _ := Articles(Input{
		SourceID:   uuid.New(),
		SourceType: "news",
		ConfigType: "rss",
		Items: []FeedItem{{
			Title: `Labour Force Survey, <span class="refper">September 2026</span>`,
			URL:   "https://example.com/lfs",
		}, {
			Title: `Forex &amp; rates &#34;watch&#34;`,
			URL:   "https://example.com/forex",
		}},
	})

	if len(candidates) != 2 {
		t.Fatalf("candidates = %d, want 2", len(candidates))
	}
	if candidates[0].Title != "Labour Force Survey, September 2026" {
		t.Errorf("Title = %q, want markup stripped", candidates[0].Title)
	}
	if candidates[1].Title != `Forex & rates "watch"` {
		t.Errorf("Title = %q, want entities decoded", candidates[1].Title)
	}
}
