package hydration

import (
	"strings"
	"testing"
)

func TestReadabilityExtractsArticleBody(t *testing.T) {
	body := []byte(`<!DOCTYPE html>
<html>
<head><title>Test Article</title></head>
<body>
<nav><a href="/">Home</a><a href="/about">About</a></nav>
<article>
<h1>Fed Holds Rates Steady</h1>
<p>The Federal Reserve held its benchmark interest rate steady today, signaling that policymakers want to see more evidence that inflation is returning to target before easing further.</p>
<p>In the statement released Wednesday, the committee noted that growth remains solid and the labor market is still tight, while price pressures have continued to moderate over the past several months.</p>
<p>Officials reiterated their data-dependent approach and declined to commit to the timing of any future change.</p>
</article>
<footer>Copyright 2026</footer>
</body>
</html>`)

	content, err := readabilityContent(body, "https://example.com/fed-holds-rates")
	if err != nil {
		t.Fatalf("readabilityContent: %v", err)
	}
	if !strings.Contains(content, "Federal Reserve held its benchmark interest rate steady") {
		t.Errorf("content missing article text: %q", content)
	}
	if strings.Contains(content, "<nav") || strings.Contains(content, "Copyright 2026") {
		t.Errorf("content still contains page chrome: %q", content)
	}
}

func TestReadabilityRejectsEmptyPage(t *testing.T) {
	body := []byte(`<html><body></body></html>`)
	if _, err := readabilityContent(body, "https://example.com/empty"); err == nil {
		t.Error("readabilityContent on empty page = nil error, want rejection")
	}
}

func TestReadabilityRejectsNavOnlyPage(t *testing.T) {
	body := []byte(`<html><body><nav><a href="/a">A</a><a href="/b">B</a></nav></body></html>`)
	if _, err := readabilityContent(body, "https://example.com/nav"); err == nil {
		t.Error("readabilityContent on nav-only page = nil error, want rejection")
	}
}

func TestExtractContentPrefersSelectorOverReadability(t *testing.T) {
	body := []byte(`<!DOCTYPE html>
<html><body>
<div class="custom-body"><p>Selector target content that is long enough to pass the floor.</p></div>
<article><p>Readability would pick this article instead if the selector were absent.</p></article>
</body></html>`)

	content, err := extractContent(body, ".custom-body", "https://example.com/article")
	if err != nil {
		t.Fatalf("extractContent: %v", err)
	}
	if !strings.Contains(content, "Selector target content") {
		t.Errorf("content = %q, want selector result", content)
	}
	if strings.Contains(content, "Readability would pick") {
		t.Errorf("content = %q, want selector to win over readability", content)
	}
}

func TestExtractContentFallsBackToReadabilityWithoutSelector(t *testing.T) {
	body := []byte(`<!DOCTYPE html>
<html><body>
<nav><a href="/">Home</a></nav>
<div class="story-body">
<p>The Bank of England left borrowing costs unchanged at its September meeting, maintaining the bank rate at current levels as policymakers weighed persistent services inflation against slowing wage growth.</p>
<p>Two members of the committee voted for an immediate cut, while the remainder preferred to wait for further data on the second-round effects of energy costs.</p>
</div>
<footer>Privacy Policy</footer>
</body></html>`)

	content, err := extractContent(body, "", "https://example.com/boe-holds")
	if err != nil {
		t.Fatalf("extractContent: %v", err)
	}
	if !strings.Contains(content, "Bank of England left borrowing costs unchanged") {
		t.Errorf("content = %q, want readability-extracted article text", content)
	}
	if strings.Contains(content, "Privacy Policy") {
		t.Errorf("content = %q, want chrome stripped", content)
	}
}

func TestExtractContentFallsBackToGenericWhenReadabilityFails(t *testing.T) {
	body := []byte(`<!DOCTYPE html>
<html><body>
<article><p>Short.</p></article>
<div><p>This container has enough paragraph text to clear the generic floor even though readability might reject the page shape for other reasons. Padding padding padding padding padding padding padding padding padding padding padding padding padding padding padding padding.</p></div>
</body></html>`)

	content, err := extractContent(body, "", "https://example.com/thin")
	if err != nil {
		t.Fatalf("extractContent: %v", err)
	}
	if content == "" {
		t.Error("extractContent returned empty content, want generic fallback")
	}
}
