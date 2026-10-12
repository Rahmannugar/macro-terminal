package hydration

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"strings"

	readability "codeberg.org/readeck/go-readability/v2"
)

// readabilityContent isolates the article body using the Mozilla Readability
// algorithm. It runs when the source has no configured content selector, and
// before the generic container heuristic. The page URL lets Readability
// resolve relative links; a parse failure or a near-empty result is rejected
// so the caller can fall back.
func readabilityContent(body []byte, pageURL string) (string, error) {
	parser := readability.NewParser()
	var parsed *url.URL
	if u, err := url.Parse(pageURL); err == nil {
		parsed = u
	}
	article, err := parser.Parse(bytes.NewReader(body), parsed)
	if err != nil {
		return "", fmt.Errorf("readability parse: %w", err)
	}
	if article.Node == nil {
		return "", errors.New("readability returned no content node")
	}
	var buffer bytes.Buffer
	if err := article.RenderHTML(&buffer); err != nil {
		return "", fmt.Errorf("render readability content: %w", err)
	}
	content := strings.TrimSpace(buffer.String())
	if content == "" {
		return "", errors.New("readability returned empty content")
	}
	if len(content) < minGenericContentLength {
		return "", errors.New("readability returned near-empty content")
	}
	return content, nil
}
