package hydration

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func parseDocument(t *testing.T, page string) *goquery.Document {
	t.Helper()
	document, err := goquery.NewDocumentFromReader(strings.NewReader(page))
	if err != nil {
		t.Fatalf("parse page: %v", err)
	}
	return document
}

func TestGenericExtractionPrefersArticleTag(t *testing.T) {
	document := parseDocument(t, `<html><body>
		<nav><p>Menu item one.</p><p>Menu item two.</p></nav>
		<article><p>`+longText+`</p><p>`+longText+`</p></article>
		<footer><p>Footer.</p></footer>
	</body></html>`)

	content, err := extractGenericContent(document)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if !strings.Contains(content, longText[:40]) {
		t.Errorf("content = %q, want the article paragraphs", content)
	}
	if strings.Contains(content, "Menu item") {
		t.Errorf("content = %q, must not include navigation", content)
	}
}

func TestGenericExtractionFallsBackToMain(t *testing.T) {
	document := parseDocument(t, `<html><body>
		<main><section><p>`+longText+`</p><p>`+longText+`</p></section></main>
	</body></html>`)

	content, err := extractGenericContent(document)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if !strings.Contains(content, longText[:40]) {
		t.Errorf("content = %q, want the main text", content)
	}
}

func TestGenericExtractionUsesDensestParagraphContainer(t *testing.T) {
	document := parseDocument(t, `<html><body>
		<div class="layout">
			<div class="sidebar"><p>`+longText[:100]+`</p></div>
			<div class="story"><p>`+longText+`</p><p>`+longText+`</p></div>
		</div>
	</body></html>`)

	content, err := extractGenericContent(document)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if !strings.Contains(content, longText[:40]) {
		t.Errorf("content = %q, want the densest container", content)
	}
}

func TestGenericExtractionRejectsThinContainers(t *testing.T) {
	document := parseDocument(t, `<html><body>
		<article><p>Too short.</p></article>
	</body></html>`)

	if _, err := extractGenericContent(document); err == nil {
		t.Fatal("extract error = nil, want a thin-container rejection")
	}
}

func TestGenericExtractionRejectsPageWithoutText(t *testing.T) {
	document := parseDocument(t, `<html><body><div>Loading…</div></body></html>`)

	if _, err := extractGenericContent(document); err == nil {
		t.Fatal("extract error = nil, want a rejection for a page without article text")
	}
}

func TestGenericExtractionStripsJSONLDScripts(t *testing.T) {
	document := parseDocument(t, `<html><body>
		<article>
			<script type="application/ld+json">{"@context":"https://schema.org","@type":"BreadcrumbList","itemListElement":[]}</script>
			<p>`+longText+`</p>
		</article>
	</body></html>`)

	content, err := extractGenericContent(document)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if !strings.Contains(content, longText[:40]) {
		t.Errorf("content = %q, want the article paragraph", content)
	}
	if strings.Contains(content, "ld+json") || strings.Contains(content, "schema.org") {
		t.Errorf("content = %q, must not include JSON-LD script", content)
	}
}

func TestGenericExtractionRejectsPageWithOnlyJSONLD(t *testing.T) {
	document := parseDocument(t, `<html><body>
		<article>
			<script type="application/ld+json">{"@context":"https://schema.org","@type":"NewsArticle","headline":"Title"}</script>
		</article>
	</body></html>`)

	if _, err := extractGenericContent(document); err == nil {
		t.Fatal("extract error = nil, want a rejection for a page whose only text is JSON-LD")
	}
}

func TestGenericExtractionStripsStyles(t *testing.T) {
	document := parseDocument(t, `<html><body>
		<article>
			<style>.hero{background:url(...)}</style>
			<p>`+longText+`</p>
		</article>
	</body></html>`)

	content, err := extractGenericContent(document)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if strings.Contains(content, "<style") {
		t.Errorf("content = %q, must not include style tag", content)
	}
}
