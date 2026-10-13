package market

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestEpochBucketAlignment(t *testing.T) {
	cases := []struct {
		name    string
		seconds int64
		input   string
		want    string
	}{
		{"five minute floor", 300, "2026-10-12T14:47:31Z", "2026-10-12T14:45:00Z"},
		{"five minute exact boundary", 300, "2026-10-12T14:45:00Z", "2026-10-12T14:45:00Z"},
		{"hour floor", 3600, "2026-10-12T14:47:31Z", "2026-10-12T14:00:00Z"},
		{"four hour floor", 14400, "2026-10-12T14:47:31Z", "2026-10-12T12:00:00Z"},
		{"four hour aligned", 14400, "2026-10-12T08:00:00Z", "2026-10-12T08:00:00Z"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			input := parseTime(t, test.input)
			got := epochBucket(test.seconds)(input)
			want := parseTime(t, test.want)
			if !got.Equal(want) {
				t.Errorf("epochBucket(%d)(%v) = %v, want %v", test.seconds, input, got, want)
			}
			if end := epochBucketEnd(test.seconds)(input); !end.Equal(want.Add(time.Duration(test.seconds) * time.Second)) {
				t.Errorf("epochBucketEnd(%d)(%v) = %v, want %v", test.seconds, input, end, want.Add(time.Duration(test.seconds)*time.Second))
			}
		})
	}
}

func TestBucketWeekTruncatesToMonday(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"2026-10-12T14:00:00Z", "2026-10-12T00:00:00Z"}, // Monday stays
		{"2026-10-14T09:30:00Z", "2026-10-12T00:00:00Z"}, // Wednesday rolls back
		{"2026-10-11T23:59:59Z", "2026-10-05T00:00:00Z"}, // Sunday belongs to prior week
		{"2026-01-01T12:00:00Z", "2025-12-29T00:00:00Z"}, // year boundary
	}
	for _, test := range cases {
		input := parseTime(t, test.input)
		got := bucketWeek(input)
		want := parseTime(t, test.want)
		if !got.Equal(want) {
			t.Errorf("bucketWeek(%v) = %v, want %v", input, got, want)
		}
		if end := (candleAggregations["1week"]).bucketEnd(input); !end.Equal(want.AddDate(0, 0, 7)) {
			t.Errorf("1week bucketEnd(%v) = %v, want %v", input, end, want.AddDate(0, 0, 7))
		}
	}
}

func TestBucketMonthTruncatesToFirstDay(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"2026-10-12T14:47:31Z", "2026-10-01T00:00:00Z"},
		{"2026-02-28T23:59:59Z", "2026-02-01T00:00:00Z"},
		{"2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z"},
	}
	for _, test := range cases {
		input := parseTime(t, test.input)
		got := bucketMonth(input)
		want := parseTime(t, test.want)
		if !got.Equal(want) {
			t.Errorf("bucketMonth(%v) = %v, want %v", input, got, want)
		}
	}
	if end := candleAggregations["1month"].bucketEnd(parseTime(t, "2026-02-10T00:00:00Z")); !end.Equal(parseTime(t, "2026-03-01T00:00:00Z")) {
		t.Errorf("1month bucketEnd = %v, want 2026-03-01", end)
	}
}

func TestCollapseBucketsNewestFirst(t *testing.T) {
	pairID := uuid.New()
	aggregation := candleAggregations["5min"]
	samples := []candleSample{
		{timestamp: parseTime(t, "2026-10-12T14:04:00Z"), open: 104, high: 106, low: 103, close: 105},
		{timestamp: parseTime(t, "2026-10-12T14:02:00Z"), open: 102, high: 108, low: 101, close: 107},
		{timestamp: parseTime(t, "2026-10-12T14:01:00Z"), open: 100, high: 105, low: 99, close: 103},
		{timestamp: parseTime(t, "2026-10-12T13:59:00Z"), open: 90, high: 95, low: 88, close: 94},
	}
	buckets := collapseBuckets(samples, aggregation, "5min", pairID)
	if len(buckets) != 2 {
		t.Fatalf("len(buckets) = %d, want 2", len(buckets))
	}
	current, ok := buckets[parseTime(t, "2026-10-12T14:00:00Z")]
	if !ok {
		t.Fatal("missing 14:00 bucket")
	}
	// Open comes from the oldest sample in the bucket, close from the newest.
	if current.Open != 100 {
		t.Errorf("Open = %v, want 100", current.Open)
	}
	if current.Close != 105 {
		t.Errorf("Close = %v, want 105", current.Close)
	}
	if current.High != 108 || current.Low != 99 {
		t.Errorf("High/Low = %v/%v, want 108/99", current.High, current.Low)
	}
	if current.Timeframe != "5min" || current.EntityPairID != pairID {
		t.Errorf("Timeframe/EntityPairID = %q/%v, want 5min/%v", current.Timeframe, current.EntityPairID, pairID)
	}
	previous, ok := buckets[parseTime(t, "2026-10-12T13:55:00Z")]
	if !ok {
		t.Fatal("missing 13:55 bucket")
	}
	if previous.Open != 90 || previous.Close != 94 || previous.High != 95 || previous.Low != 88 {
		t.Errorf("13:55 bucket = %+v, want O90 H95 L88 C94", previous)
	}
}

func TestCollapseBucketsSingleSampleKeepsValues(t *testing.T) {
	buckets := collapseBuckets(
		[]candleSample{{timestamp: parseTime(t, "2026-10-12T14:01:00Z"), open: 100, high: 110, low: 95, close: 107}},
		candleAggregations["1h"],
		"1h",
		uuid.New(),
	)
	bucket, ok := buckets[parseTime(t, "2026-10-12T14:00:00Z")]
	if !ok {
		t.Fatal("missing 14:00 bucket")
	}
	if bucket.Open != 100 || bucket.High != 110 || bucket.Low != 95 || bucket.Close != 107 {
		t.Errorf("bucket = %+v, want O100 H110 L95 C107", bucket)
	}
}

func parseTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse %q: %v", value, err)
	}
	return parsed
}
