package normalization

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// CalendarEvent is one parsed calendar row before indicator
// classification. Values stay nil when the provider sent nothing usable.
type CalendarEvent struct {
	Name        string
	ScheduledAt time.Time
	Previous    *float64
	Consensus   *float64
	Actual      *float64
	CountryCode string
	Currency    string
	Importance  string
}

// CalendarStats records what a calendar payload contained: rows seen,
// rows too broken to use, non-calendar rows that traveled to the news
// pipeline, and non-calendar rows that could not.
type CalendarStats struct {
	Rows        int
	Malformed   int
	NewsRows    int
	NewsSkipped int
}

// leadingNumber matches the first plain number in a value like "2.9% YoY",
// "+0.3", or "233K". Units and suffixes stay with the provider — the
// leading number is what the calendar column stores.
var leadingNumber = regexp.MustCompile(`^[+-]?(?:\d+(?:\.\d*)?|\.\d+)`)

type calendarNewsRow struct {
	title string
	url   string
}

// CalendarEvents parses a calendar payload into rows awaiting
// classification plus non-calendar rows that belong to the news pipeline.
// Three shapes are recognized: an object carrying an "events" array, an
// object carrying a "data" array (US-only agencies, so rows default to
// the United States), and a bare array of rows typed as indicators. A
// non-calendar row only travels to news when its URL appears exactly once
// in the batch — a constant source URL would otherwise collapse the batch
// into one junk candidate.
func CalendarEvents(body []byte) ([]CalendarEvent, []FeedItem, CalendarStats) {
	var stats CalendarStats
	trimmed := strings.TrimSpace(string(body))

	var rows []map[string]any
	switch {
	case strings.HasPrefix(trimmed, "["):
		if err := json.Unmarshal([]byte(trimmed), &rows); err != nil {
			stats.Malformed++
			return nil, nil, stats
		}
	case strings.HasPrefix(trimmed, "{"):
		var envelope struct {
			Events []map[string]any `json:"events"`
			Data   []map[string]any `json:"data"`
		}
		switch {
		case json.Unmarshal([]byte(trimmed), &envelope) != nil:
			stats.Malformed++
			return nil, nil, stats
		case envelope.Events != nil:
			rows = envelope.Events
		case envelope.Data != nil:
			rows = envelope.Data
		default:
			stats.Malformed++
			return nil, nil, stats
		}
	default:
		stats.Malformed++
		return nil, nil, stats
	}

	stats.Rows = len(rows)
	events := make([]CalendarEvent, 0, len(rows))
	newsRows := make([]calendarNewsRow, 0, len(rows))
	for _, row := range rows {
		event, ok := parseCalendarRow(row)
		if !ok {
			stats.Malformed++
			continue
		}
		if rowIsNonCalendar(row) {
			newsRows = append(newsRows, calendarNewsRow{
				title: event.Name,
				url:   strings.TrimSpace(textValue(rowValue(row, "url", "sourceUrl"))),
			})
			continue
		}
		events = append(events, event)
	}

	urlCounts := map[string]int{}
	for _, row := range newsRows {
		if row.url != "" {
			urlCounts[row.url]++
		}
	}

	news := make([]FeedItem, 0, len(newsRows))
	for _, row := range newsRows {
		if row.url == "" || urlCounts[row.url] != 1 {
			stats.NewsSkipped++
			continue
		}
		news = append(news, FeedItem{Title: row.title, URL: row.url})
		stats.NewsRows++
	}
	return events, news, stats
}

// parseCalendarRow reads the fields the provider shapes share: a name, a
// schedule, optional previous/consensus/actual values, and the country,
// currency, and importance tags the calendar filters rely on. A row
// without a name or a usable time is malformed.
func parseCalendarRow(row map[string]any) (CalendarEvent, bool) {
	event := CalendarEvent{
		Name:        strings.TrimSpace(textValue(row["name"])),
		Previous:    numberValue(rowValue(row, "previous", "prior")),
		Consensus:   numberValue(rowValue(row, "consensus", "forecast")),
		Actual:      numberValue(rowValue(row, "actual")),
		ScheduledAt: scheduledValue(row),
		CountryCode: strings.ToUpper(strings.TrimSpace(textValue(row["countryCode"]))),
		Currency:    strings.ToUpper(strings.TrimSpace(textValue(row["currency"]))),
		Importance:  importanceValue(rowValue(row, "importance", "impact")),
	}
	if event.Name == "" {
		event.Name = strings.TrimSpace(textValue(row["title"]))
	}
	if event.Name == "" {
		event.Name = strings.TrimSpace(textValue(row["eventName"]))
	}
	if event.Name == "" || event.ScheduledAt.IsZero() {
		return CalendarEvent{}, false
	}
	if event.CountryCode == "" {
		event.CountryCode = "US"
	}
	if event.Currency == "" {
		event.Currency = "USD"
	}
	return event, true
}

// importanceValue normalizes the level words providers use ("med" versus
// "medium") so one filter vocabulary covers every calendar source.
func importanceValue(value any) string {
	switch strings.ToLower(strings.TrimSpace(textValue(value))) {
	case "high":
		return "high"
	case "medium", "med":
		return "medium"
	case "low":
		return "low"
	default:
		return ""
	}
}

// rowIsNonCalendar reports rows that are not economic indicators:
// speeches, summits, holidays, and the like belong to news, not the
// calendar store.
func rowIsNonCalendar(row map[string]any) bool {
	if value := strings.TrimSpace(textValue(row["type"])); value != "" {
		return !strings.EqualFold(value, "indicator")
	}
	if value := strings.TrimSpace(textValue(row["category"])); value != "" {
		return !strings.EqualFold(value, "economic-indicators")
	}
	return false
}

func rowValue(row map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := row[key]; ok && value != nil {
			return value
		}
	}
	return nil
}

func scheduledValue(row map[string]any) time.Time {
	for _, key := range []string{"time_utc", "time", "scheduledAt"} {
		if value, ok := row[key]; ok && value != nil {
			if parsed, err := time.Parse(time.RFC3339, textValue(value)); err == nil {
				return parsed
			}
		}
	}
	if value, ok := row["date"]; ok && value != nil {
		if parsed, err := time.Parse("2006-01-02", textValue(value)); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}

func textValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return ""
	}
}

// numberValue keeps the leading plain number from either a JSON number or
// a decorated string; anything else becomes nil, because indicator values
// are optional.
func numberValue(value any) *float64 {
	switch typed := value.(type) {
	case float64:
		result := typed
		return &result
	case string:
		match := leadingNumber.FindString(strings.TrimSpace(typed))
		if match == "" {
			return nil
		}
		parsed, err := strconv.ParseFloat(match, 64)
		if err != nil {
			return nil
		}
		return &parsed
	default:
		return nil
	}
}
