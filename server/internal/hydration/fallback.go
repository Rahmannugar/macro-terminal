package hydration

import (
	"errors"
	"fmt"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// minGenericContentLength is the plain-text floor for the generic
// extraction: teaser boxes, paywall stubs, and nav-only containers fall
// under it and are rejected instead of stored.
const minGenericContentLength = 160

var genericContainers = []string{"article", "main", "[role=main]"}

// extractGenericContent finds the article body without a configured
// selector: semantic containers first, then the element holding the most
// paragraph text. Every candidate must clear the text floor.
func extractGenericContent(document *goquery.Document) (string, error) {
	for _, selector := range genericContainers {
		node := document.Find(selector).First()
		if node.Length() == 0 {
			continue
		}
		if content, err := containerContent(node); err == nil {
			return content, nil
		}
	}
	container, ok := densestParagraphContainer(document)
	if !ok {
		return "", errors.New("no container holds enough article text")
	}
	return containerContent(container)
}

func containerContent(node *goquery.Selection) (string, error) {
	if len(strings.TrimSpace(node.Text())) < minGenericContentLength {
		return "", errors.New("container holds too little text")
	}
	html, err := node.Html()
	if err != nil {
		return "", fmt.Errorf("read content container: %w", err)
	}
	content := strings.TrimSpace(html)
	if content == "" {
		return "", errors.New("content container is empty")
	}
	return content, nil
}

// densestParagraphContainer scores every element that directly contains
// paragraphs by their combined text length. Scoring parents of p elements —
// rather than all containers — keeps outer page wrappers from winning on
// the strength of the article buried inside them.
func densestParagraphContainer(document *goquery.Document) (*goquery.Selection, bool) {
	type candidate struct {
		node *goquery.Selection
		text int
	}
	byNode := map[*html.Node]*candidate{}
	document.Find("p").Each(func(_ int, paragraph *goquery.Selection) {
		text := len(strings.TrimSpace(paragraph.Text()))
		if text == 0 {
			return
		}
		parent := paragraph.Parent()
		if parent.Length() == 0 || parent.Is("body, html") {
			return
		}
		entry, ok := byNode[parent.Nodes[0]]
		if !ok {
			entry = &candidate{node: parent}
			byNode[parent.Nodes[0]] = entry
		}
		entry.text += text
	})
	var best *candidate
	for _, entry := range byNode {
		if best == nil || entry.text > best.text {
			best = entry
		}
	}
	if best == nil || best.text < minGenericContentLength {
		return nil, false
	}
	return best.node, true
}
