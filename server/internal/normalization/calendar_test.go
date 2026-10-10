package normalization

import (
	"testing"
	"time"
)

func TestCalendarEventsParsesFinanceCalendarShape(t *testing.T) {
	body := []byte(`{
		"date": "2026-10-02",
		"market": "US",
		"events": [
			{
				"date": "2026-10-02",
				"time_utc": "2026-10-02T12:30:00Z",
				"time_et": "08:30",
				"all_day": false,
				"name": "CPI y/y",
				"title": "Consumer Price Index y/y",
				"impact": "high",
				"category": "economic-indicators",
				"consensus": "2.9% YoY",
				"prior": "3.0% YoY",
				"actual": "2.8% YoY",
				"url": "https://www.financecalendar.com/event/cpi-us/"
			},
			{
				"date": "2026-10-05",
				"time_utc": null,
				"all_day": true,
				"name": "German Unity Day",
				"category": "holidays",
				"prior": "Closed for national holiday",
				"url": "https://www.financecalendar.com/event/german-unity-day/"
			}
		]
	}`)

	events, news, stats := CalendarEvents(body)

	if stats.Rows != 2 || stats.Malformed != 0 {
		t.Fatalf("stats = %+v, want 2 rows, 0 malformed", stats)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1 calendar event", len(events))
	}
	cpi := events[0]
	if cpi.Name != "CPI y/y" {
		t.Errorf("Name = %q, want CPI y/y", cpi.Name)
	}
	wantScheduled := time.Date(2026, 10, 2, 12, 30, 0, 0, time.UTC)
	if !cpi.ScheduledAt.Equal(wantScheduled) {
		t.Errorf("ScheduledAt = %v, want %v", cpi.ScheduledAt, wantScheduled)
	}
	assertFloat(t, "Previous", cpi.Previous, 3.0)
	assertFloat(t, "Consensus", cpi.Consensus, 2.9)
	assertFloat(t, "Actual", cpi.Actual, 2.8)

	if len(news) != 1 {
		t.Fatalf("news = %d, want the holiday row", len(news))
	}
	if news[0].Title != "German Unity Day" || news[0].URL != "https://www.financecalendar.com/event/german-unity-day/" {
		t.Errorf("news[0] = %+v, want holiday title and URL", news[0])
	}
	if stats.NewsRows != 1 || stats.NewsSkipped != 0 {
		t.Errorf("news stats = %d/%d, want 1/0", stats.NewsRows, stats.NewsSkipped)
	}
}

func TestCalendarEventsParsesFinanceCalendarAllDayRow(t *testing.T) {
	body := []byte(`{"events": [
		{"date": "2026-10-05", "time_utc": null, "all_day": true,
		 "name": "German Unity Day", "category": "holidays",
		 "url": "https://www.financecalendar.com/event/german-unity-day/"}
	]}`)

	events, _, stats := CalendarEvents(body)

	if stats.Malformed != 0 || len(events) != 0 {
		t.Fatalf("all-day holiday should route to news, got events=%d stats=%+v", len(events), stats)
	}
}

func TestCalendarEventsParsesBiquoteShape(t *testing.T) {
	body := []byte(`[
		{"id": "a", "time": "2026-10-02T12:30:00Z", "countryCode": "US",
		 "currency": "USD", "name": "Core CPI m/m", "importance": "3",
		 "type": "indicator", "unit": "percent",
		 "actual": 0.3, "forecast": 0.3, "previous": 0.2,
		 "sourceUrl": "https://www.fxempire.com/economic-calendar/core-cpi-mom-us",
		 "source": "fxempire"},
		{"id": "b", "time": "2026-10-04T14:00:00Z", "type": "speech",
		 "name": "Fed Chair Powell remarks",
		 "sourceUrl": "https://www.fxempire.com/economic-calendar/powell",
		 "source": "fxempire"}
	]`)

	events, news, stats := CalendarEvents(body)

	if stats.Rows != 2 || stats.Malformed != 0 {
		t.Fatalf("stats = %+v, want 2 rows, 0 malformed", stats)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1 indicator row", len(events))
	}
	assertFloat(t, "Actual", events[0].Actual, 0.3)
	assertFloat(t, "Consensus", events[0].Consensus, 0.3)
	assertFloat(t, "Previous", events[0].Previous, 0.2)

	if stats.NewsRows != 1 || stats.NewsSkipped != 0 {
		t.Errorf("news stats = %d/%d, want 1/0", stats.NewsRows, stats.NewsSkipped)
	}
	if len(news) != 1 || news[0].Title != "Fed Chair Powell remarks" {
		t.Errorf("news = %+v, want Powell speech", news)
	}
}

func TestCalendarEventsDropsBiquoteRowsSharingSourceURL(t *testing.T) {
	body := []byte(`[
		{"time": "2026-10-04T14:00:00Z", "type": "speech",
		 "name": "Speech one", "sourceUrl": "https://www.fxempire.com/calendar"},
		{"time": "2026-10-04T15:00:00Z", "type": "summit",
		 "name": "Summit two", "sourceUrl": "https://www.fxempire.com/calendar"}
	]`)

	events, news, stats := CalendarEvents(body)

	if len(events) != 0 || len(news) != 0 {
		t.Fatalf("events=%d news=%d, want both dropped", len(events), len(news))
	}
	if stats.NewsSkipped != 2 || stats.NewsRows != 0 {
		t.Errorf("news stats = %d/%d, want 0/2", stats.NewsRows, stats.NewsSkipped)
	}
}

func TestCalendarEventsDropsRowsWithoutNameOrTime(t *testing.T) {
	body := []byte(`{"events": [
		{"date": "2026-10-02", "time_utc": "2026-10-02T12:30:00Z", "category": "economic-indicators"},
		{"name": "Mystery event", "category": "economic-indicators"},
		{"name": "Valid row", "time_utc": "2026-10-02T14:00:00Z", "category": "economic-indicators"}
	]}`)

	events, _, stats := CalendarEvents(body)

	if stats.Malformed != 2 {
		t.Errorf("Malformed = %d, want 2", stats.Malformed)
	}
	if len(events) != 1 || events[0].Name != "Valid row" {
		t.Errorf("events = %+v, want only the valid row", events)
	}
}

func TestCalendarEventsRejectsUnusableBodies(t *testing.T) {
	for _, body := range [][]byte{nil, []byte(""), []byte("not json"), []byte(`{"rows": []}`)} {
		events, news, stats := CalendarEvents(body)
		if len(events) != 0 || len(news) != 0 {
			t.Errorf("body %q: expected no rows", body)
		}
		if stats.Malformed == 0 {
			t.Errorf("body %q: Malformed = 0, want a malformed marker", body)
		}
	}
}

func TestCalendarEventsKeepsLeadingNumbersOnly(t *testing.T) {
	body := []byte(`{"events": [
		{"name": "Retail Sales m/m", "time_utc": "2026-10-02T12:30:00Z",
		 "category": "economic-indicators", "consensus": "+0.4%", "prior": "233K"},
		{"name": "Holiday", "time_utc": "2026-10-02T12:30:00Z",
		 "category": "economic-indicators", "actual": "Closed for national holiday"},
		{"name": "Empty", "time_utc": "2026-10-02T12:30:00Z",
		 "category": "economic-indicators", "actual": ""}
	]}`)

	events, _, _ := CalendarEvents(body)

	if len(events) != 3 {
		t.Fatalf("events = %d, want 3", len(events))
	}
	assertFloat(t, "Consensus", events[0].Consensus, 0.4)
	assertFloat(t, "Previous", events[0].Previous, 233)
	if events[1].Actual != nil {
		t.Errorf("Actual = %v, want nil for prose value", *events[1].Actual)
	}
	if events[2].Actual != nil {
		t.Errorf("Actual = %v, want nil for empty value", *events[2].Actual)
	}
}

func assertFloat(t *testing.T, name string, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s = nil, want %v", name, want)
	}
	if *got != want {
		t.Errorf("%s = %v, want %v", name, *got, want)
	}
}

func TestCalendarEventsParsesXoomarShape(t *testing.T) {
	body := []byte(`{"data": [
		{"source": "bls", "eventName": "CPI (Consumer Price Index)",
		 "importance": "high", "scheduledAt": "2026-10-14T12:30:00.000Z",
		 "periodLabel": "September 2026", "previous": null,
		 "forecast": null, "actual": null, "unit": "% y/y"},
		{"source": "dol", "eventName": "Initial Jobless Claims",
		 "importance": "med", "scheduledAt": "2026-10-15T12:30:00.000Z",
		 "previous": 218000, "forecast": null, "actual": 215000}
	]}`)

	events, news, stats := CalendarEvents(body)

	if stats.Rows != 2 || stats.Malformed != 0 {
		t.Fatalf("stats = %+v, want 2 rows, 0 malformed", stats)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	if len(news) != 0 {
		t.Errorf("news = %d, want 0", len(news))
	}
	first := events[0]
	if first.Name != "CPI (Consumer Price Index)" {
		t.Errorf("name = %q, want the eventName field", first.Name)
	}
	if first.CountryCode != "US" || first.Currency != "USD" {
		t.Errorf("country/currency = %q/%q, want US/USD defaults", first.CountryCode, first.Currency)
	}
	if first.Importance != "high" {
		t.Errorf("importance = %q, want high", first.Importance)
	}
	if want := time.Date(2026, 10, 14, 12, 30, 0, 0, time.UTC); !first.ScheduledAt.Equal(want) {
		t.Errorf("scheduled = %s, want %s", first.ScheduledAt, want)
	}
	if events[1].Importance != "medium" {
		t.Errorf("importance = %q, want med normalized to medium", events[1].Importance)
	}
	assertFloat(t, "Previous", events[1].Previous, 218000)
	assertFloat(t, "Actual", events[1].Actual, 215000)
}
