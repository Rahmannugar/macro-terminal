package normalization

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// ieaListing mirrors the structure of iea.org/news: the item element is the
// anchor itself, with title and date inside it.
const ieaListing = `<html><body>
<div class="m-grid m-grid--news-detailed">
  <article><div class="m-news-detailed-listing">
    <a href="/news/first-story" class="m-news-detailed-listing__link">
      <div class="m-news-detailed-listing__content">
        <h5 class="m-news-detailed-listing__title f-title-8"><span class="m-news-detailed-listing__hover">  First   story headline </span></h5>
      </div>
      <div class="m-news-detailed-listing__date f-ui-2"> 29 September 2026 </div>
    </a>
  </div></article>
  <article><div class="m-news-detailed-listing">
    <a href="/news/second-story" class="m-news-detailed-listing__link">
      <div class="m-news-detailed-listing__content">
        <h5 class="m-news-detailed-listing__title f-title-8"><span class="m-news-detailed-listing__hover">Second story headline</span></h5>
      </div>
      <div class="m-news-detailed-listing__date f-ui-2">1 October 2026</div>
    </a>
  </div></article>
</div>
</body></html>`

// pbocListing mirrors the PBoC year page: the item is a list row, the
// title-bearing link and the date sit in a shared inner container.
const pbocListing = `<html><body>
<ul class="prhhul">
  <li>
    <div class="prhhd1">
      <a href="/en/3688110/3688172/2026/2026093015565447786/index.html" class="ListL"><img src="/en/imageDir/x.jpg"></a>
      <div class="ListR">
        <span class="prhhdata">2026-09-29</span>
        <a href="/en/3688110/3688172/2026/2026093015565447786/index.html" title="PBOC Adjusts Tools" istitle="true">PBOC Adjusts and Improves Several Monetary Policy Tools</a>
      </div>
    </div>
  </li>
  <li>
    <div class="prhhd1">
      <a href="/en/3688110/3688172/2026/2026092916474264671/index.html" class="ListL"><img src="/en/imageDir/y.jpg"></a>
      <div class="ListR">
        <span class="prhhdata">2026-09-29</span>
        <a href="/en/3688110/3688172/2026/2026092916474264671/index.html" title="Governor Meets EU Ambassador" istitle="true">Governor Pan Gongsheng Meets with EU Ambassador</a>
      </div>
    </div>
  </li>
</ul>
</body></html>`

var ieaSelectors = Selectors{
	Item:       ".m-news-detailed-listing__link",
	Title:      ".m-news-detailed-listing__title",
	Date:       ".m-news-detailed-listing__date",
	DateLayout: "2 January 2006",
}

var pbocSelectors = Selectors{
	Item:       "ul.prhhul li",
	Title:      ".ListR a",
	URL:        ".ListR a",
	Date:       ".prhhdata",
	DateLayout: "2006-01-02",
}

func TestArticlesExtractsIEAListing(t *testing.T) {
	candidates, stats := Articles(Input{
		SourceID:   uuid.Nil,
		SourceType: "official",
		ConfigType: "web",
		Body:       []byte(ieaListing),
		BaseURL:    "https://www.iea.org/news",
		Selectors:  ieaSelectors,
	})

	if stats != (Stats{Candidates: 2}) {
		t.Fatalf("stats = %+v, want 2/0/0", stats)
	}
	if candidates[0].Title != "First story headline" {
		t.Errorf("title = %q, want collapsed whitespace", candidates[0].Title)
	}
	if candidates[0].URL != "https://www.iea.org/news/first-story" {
		t.Errorf("URL = %q, want resolved against the page URL", candidates[0].URL)
	}
	if candidates[0].PublishedAt != time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC) {
		t.Errorf("PublishedAt = %v, want 2026-09-29", candidates[0].PublishedAt)
	}
	if candidates[1].URL != "https://www.iea.org/news/second-story" {
		t.Errorf("second URL = %q", candidates[1].URL)
	}
}

func TestArticlesExtractsPBOCListing(t *testing.T) {
	candidates, stats := Articles(Input{
		SourceID:   uuid.Nil,
		SourceType: "official",
		ConfigType: "web",
		Body:       []byte(pbocListing),
		BaseURL:    "https://www.pbc.gov.cn/en/3688110/3688172/2026/index.html",
		Selectors:  pbocSelectors,
	})

	if stats != (Stats{Candidates: 2}) {
		t.Fatalf("stats = %+v, want 2/0/0", stats)
	}
	if candidates[0].Title != "PBOC Adjusts and Improves Several Monetary Policy Tools" {
		t.Errorf("title = %q, want the title-bearing link's text", candidates[0].Title)
	}
	if candidates[0].URL != "https://www.pbc.gov.cn/en/3688110/3688172/2026/2026093015565447786/index.html" {
		t.Errorf("URL = %q", candidates[0].URL)
	}
	if candidates[0].PublishedAt.IsZero() {
		t.Error("PublishedAt = zero, want 2026-09-29")
	}
}

func TestArticlesWebWithoutSelectorsYieldsNothing(t *testing.T) {
	candidates, stats := Articles(Input{
		SourceID:   uuid.Nil,
		SourceType: "official",
		ConfigType: "web",
		Body:       []byte(ieaListing),
		BaseURL:    "https://www.iea.org/news",
	})

	if len(candidates) != 0 || stats != (Stats{}) {
		t.Errorf("candidates = %d stats = %+v, want none without configured selectors", len(candidates), stats)
	}
}

func TestArticlesWebDropsEntriesWithoutTitleOrLink(t *testing.T) {
	page := `<html><body>
<a href="/news/has-title" class="m-news-detailed-listing__link"><h5 class="m-news-detailed-listing__title">Has title</h5></a>
<a href="/news/no-title" class="m-news-detailed-listing__link"><h5 class="m-news-detailed-listing__title">   </h5></a>
<a class="m-news-detailed-listing__link"><h5 class="m-news-detailed-listing__title">No link</h5></a>
</body></html>`

	candidates, stats := Articles(Input{
		SourceID:   uuid.Nil,
		SourceType: "official",
		ConfigType: "web",
		Body:       []byte(page),
		BaseURL:    "https://www.iea.org/news",
		Selectors:  ieaSelectors,
	})

	if len(candidates) != 1 || stats != (Stats{Candidates: 1, Invalid: 2}) {
		t.Errorf("candidates = %d stats = %+v, want 1/0/2", len(candidates), stats)
	}
}

func TestArticlesWebKeepsEntryWithUnparsableDate(t *testing.T) {
	page := `<html><body>
<a href="/news/story" class="m-news-detailed-listing__link"><h5 class="m-news-detailed-listing__title">Story</h5><div class="m-news-detailed-listing__date">31 Feb 2026</div></a>
</body></html>`

	candidates, stats := Articles(Input{
		SourceID:   uuid.Nil,
		SourceType: "official",
		ConfigType: "web",
		Body:       []byte(page),
		BaseURL:    "https://www.iea.org/news",
		Selectors:  ieaSelectors,
	})

	if len(candidates) != 1 || stats != (Stats{Candidates: 1}) {
		t.Fatalf("candidates = %d stats = %+v, want the entry kept", len(candidates), stats)
	}
	if !candidates[0].PublishedAt.IsZero() {
		t.Errorf("PublishedAt = %v, want zero on unparsable date", candidates[0].PublishedAt)
	}
}

func TestArticlesWebSelectorMatchingNothingYieldsNothing(t *testing.T) {
	candidates, stats := Articles(Input{
		SourceID:   uuid.Nil,
		SourceType: "official",
		ConfigType: "web",
		Body:       []byte(`<html><body><p class="unrelated">Nothing here</p></body></html>`),
		BaseURL:    "https://www.iea.org/news",
		Selectors:  ieaSelectors,
	})

	if len(candidates) != 0 || stats != (Stats{}) {
		t.Errorf("candidates = %d stats = %+v, want none", len(candidates), stats)
	}
}

func TestParseSelectors(t *testing.T) {
	full := ParseSelectors([]byte(`{"url":"https://www.iea.org/news","selectors":{"item":".a","title":".b","url":".c","date":".d","date_layout":"2 January 2006"}}`))
	if full != (Selectors{Item: ".a", Title: ".b", URL: ".c", Date: ".d", DateLayout: "2 January 2006"}) {
		t.Errorf("selectors = %+v", full)
	}

	if got := ParseSelectors([]byte(`{"url":"https://www.iea.org/news"}`)); got != (Selectors{}) {
		t.Errorf("selectors without key = %+v, want zero", got)
	}
}
