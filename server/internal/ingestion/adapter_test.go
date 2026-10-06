package ingestion

import (
	"errors"
	"testing"
)

const sampleRSS = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/">
  <channel>
    <title>Example Feed</title>
    <link>https://example.com</link>
    <description>Example</description>
    <item>
      <title>Fed holds rates steady</title>
      <link>https://example.com/fed</link>
      <guid>urn:fed-1</guid>
      <description>The Federal Reserve kept rates unchanged.</description>
      <content:encoded><![CDATA[<p>The Federal Reserve held its benchmark rate steady, citing resilient growth.</p>]]></content:encoded>
      <pubDate>Tue, 29 Sep 2026 12:30:00 GMT</pubDate>
    </item>
    <item>
      <title>Second story</title>
      <link>https://example.com/two</link>
      <description>Without guid or date.</description>
    </item>
  </channel>
</rss>`

func TestRSSAdapterParse(t *testing.T) {
	result, err := (rssAdapter{}).Parse([]byte(sampleRSS))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(result.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(result.Items))
	}

	first := result.Items[0]
	if first.GUID != "urn:fed-1" {
		t.Fatalf("first GUID = %q, want urn:fed-1", first.GUID)
	}
	if first.Title != "Fed holds rates steady" {
		t.Fatalf("first Title = %q", first.Title)
	}
	if first.URL != "https://example.com/fed" {
		t.Fatalf("first URL = %q", first.URL)
	}
	if first.Content != "<p>The Federal Reserve held its benchmark rate steady, citing resilient growth.</p>" {
		t.Fatalf("first Content = %q, want the feed's full content", first.Content)
	}
	if first.Summary != "The Federal Reserve kept rates unchanged." {
		t.Fatalf("first Summary = %q", first.Summary)
	}
	if first.Published.IsZero() {
		t.Fatalf("first Published should be parsed from pubDate")
	}

	second := result.Items[1]
	if second.GUID != "https://example.com/two" {
		t.Fatalf("GUID should fall back to link, got %q", second.GUID)
	}
	if second.Content != "" {
		t.Fatalf("second Content = %q, want empty without content:encoded", second.Content)
	}
	if !second.Published.IsZero() {
		t.Fatalf("second Published should be zero without a date")
	}
}

func TestRSSAdapterMalformed(t *testing.T) {
	if _, err := (rssAdapter{}).Parse([]byte("this is not a feed")); !errors.Is(err, ErrMalformed) {
		t.Fatalf("error = %v, want %v", err, ErrMalformed)
	}
}

func TestAPIAdapterParse(t *testing.T) {
	result, err := (apiAdapter{}).Parse([]byte(`{"status":"ok","series":["PAYEMS"]}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(result.Body) == 0 {
		t.Fatalf("Body should carry the payload")
	}

	xmlBody := `<?xml version="1.0"?><message:Structure></message:Structure>`
	xmlResult, err := (apiAdapter{}).Parse([]byte(xmlBody))
	if err != nil {
		t.Fatalf("Parse XML: %v", err)
	}
	if string(xmlResult.Body) != xmlBody {
		t.Fatalf("Body = %q, want the XML payload", xmlResult.Body)
	}

	if _, err := (apiAdapter{}).Parse([]byte("<html>rate limited</html>")); !errors.Is(err, ErrMalformed) {
		t.Fatalf("error = %v, want %v", err, ErrMalformed)
	}
}

func TestWebAdapterParse(t *testing.T) {
	result, err := (webAdapter{}).Parse([]byte("<html><body>release</body></html>"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if string(result.Body) != "<html><body>release</body></html>" {
		t.Fatalf("Body = %q", result.Body)
	}
}

func TestAdapterFor(t *testing.T) {
	for _, kind := range []string{"rss", "api", "web"} {
		if _, err := AdapterFor(kind); err != nil {
			t.Fatalf("AdapterFor(%q): %v", kind, err)
		}
	}
	if _, err := AdapterFor("ftp"); !errors.Is(err, ErrUnsupportedAdapter) {
		t.Fatalf("AdapterFor(ftp) error = %v, want %v", err, ErrUnsupportedAdapter)
	}
}
