package ingestion

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/mmcdole/gofeed"
)

type rssAdapter struct{}

func (rssAdapter) Kind() string { return "rss" }

func (rssAdapter) Parse(body []byte) (Result, error) {
	feed, err := gofeed.NewParser().Parse(bytes.NewReader(body))
	if err != nil {
		return Result{}, fmt.Errorf("%w: parse feed: %w", ErrMalformed, err)
	}
	items := make([]Item, 0, len(feed.Items))
	for _, entry := range feed.Items {
		if entry == nil {
			continue
		}
		item := Item{
			GUID:    entry.GUID,
			Title:   entry.Title,
			URL:     entry.Link,
			Content: entry.Content,
			Summary: entry.Description,
		}
		// Some feeds have no GUID; the link serves as the ID instead.
		if item.GUID == "" {
			item.GUID = entry.Link
		}
		switch {
		case entry.Image != nil && entry.Image.URL != "":
			item.ImageURL = entry.Image.URL
		default:
			for _, enclosure := range entry.Enclosures {
				if enclosure != nil && strings.HasPrefix(enclosure.Type, "image/") {
					item.ImageURL = enclosure.URL
					break
				}
			}
		}
		switch {
		case entry.PublishedParsed != nil:
			item.Published = *entry.PublishedParsed
		case entry.UpdatedParsed != nil:
			item.Published = *entry.UpdatedParsed
		}
		items = append(items, item)
	}
	return Result{Items: items}, nil
}
