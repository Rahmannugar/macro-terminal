package normalization

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// Selectors configures CSS extraction of one listing page: which repeated
// element holds an item, and where the title, link, and date live inside it.
// Configured per source configuration under the "selectors" key.
type Selectors struct {
	Item       string `json:"item"`                  // repeated element, one per article (required)
	Title      string `json:"title"`                 // element holding the headline (required)
	URL        string `json:"url"`                   // element holding the link; empty = the item itself, else the first link inside it
	Date       string `json:"date"`                  // element holding the publication date; empty = no date
	DateLayout string `json:"date_layout,omitempty"` // Go layout for the date text; empty = 2006-01-02
	Content    string `json:"content,omitempty"`     // element holding the article body on a linked article page; empty = no body hydration
}

// ParseSelectors reads the "selectors" object out of a source configuration.
// A configuration without one yields zero Selectors — extraction stays off.
func ParseSelectors(config []byte) Selectors {
	var document struct {
		Selectors Selectors `json:"selectors"`
	}
	_ = json.Unmarshal(config, &document)
	return document.Selectors
}

// extractWeb pulls article entries out of a fetched HTML listing page.
// Titles and links that the selectors cannot find produce entries the shared
// pipeline then drops as invalid; a page with no matching items yields none.
// Relative links come out as-is and resolve against the request URL later.
func extractWeb(body []byte, selectors Selectors) []FeedItem {
	if selectors.Item == "" || selectors.Title == "" {
		return nil
	}
	document, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil
	}

	layout := selectors.DateLayout
	if layout == "" {
		layout = "2006-01-02"
	}

	items := make([]FeedItem, 0, document.Find(selectors.Item).Length())
	document.Find(selectors.Item).Each(func(_ int, node *goquery.Selection) {
		items = append(items, FeedItem{
			Title:     collapseWhitespace(node.Find(selectors.Title).First().Text()),
			URL:       itemLink(node, selectors),
			Published: itemDate(node, selectors, layout),
		})
	})
	return items
}

// itemLink reads the entry's link: the configured element when set, the item
// itself when it is an anchor, otherwise the first link inside the item.
func itemLink(node *goquery.Selection, selectors Selectors) string {
	if selectors.URL != "" {
		return strings.TrimSpace(node.Find(selectors.URL).First().AttrOr("href", ""))
	}
	if href := strings.TrimSpace(node.AttrOr("href", "")); href != "" {
		return href
	}
	return strings.TrimSpace(node.Find("a[href]").First().AttrOr("href", ""))
}

// itemDate reads the publication date. An unparsable date keeps the entry
// with no date rather than dropping a real article over format drift.
func itemDate(node *goquery.Selection, selectors Selectors, layout string) time.Time {
	if selectors.Date == "" {
		return time.Time{}
	}
	text := collapseWhitespace(node.Find(selectors.Date).First().Text())
	if text == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(layout, text)
	if err != nil {
		return time.Time{}
	}
	return parsed
}
