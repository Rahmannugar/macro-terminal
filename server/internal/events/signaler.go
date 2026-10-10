package events

import (
	"context"

	"github.com/redis/go-redis/v9"
)

// FeedStreamKey is the Redis Stream carrying live invalidation signals.
const FeedStreamKey = "macro_terminal:events"

// feedSignalMaxLen bounds the stream so slow or absent readers never grow
// memory without limit; approximate trimming keeps the XADD cheap.
const feedSignalMaxLen = 1000

// FeedSignaler publishes feed invalidation signals to the event stream. The
// signal carries no article data; clients react by refetching the first feed
// page. A nil receiver publishes nothing, so callers can wire it optionally.
type FeedSignaler struct {
	client *redis.Client
}

// NewFeedSignaler builds a publisher on the shared Redis client.
func NewFeedSignaler(client *redis.Client) *FeedSignaler {
	return &FeedSignaler{client: client}
}

// NotifyFeedChanged appends one feed signal to the event stream.
func (signaler *FeedSignaler) NotifyFeedChanged(ctx context.Context) error {
	if signaler == nil || signaler.client == nil {
		return nil
	}
	return signaler.client.XAdd(ctx, &redis.XAddArgs{
		Stream: FeedStreamKey,
		MaxLen: feedSignalMaxLen,
		Approx: true,
		Values: map[string]any{"type": "feed"},
	}).Err()
}
