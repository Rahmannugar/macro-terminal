package telemetry

import (
	"context"
	"log/slog"
	"sync"

	"go.opentelemetry.io/otel/trace"
)

type requestIDContextKey struct{}
type requestLogAttributesContextKey struct{}

type Completion struct {
	Event   string
	Message string
}

type CompletionDetails struct {
	Success  Completion
	Rejected Completion
	Failed   Completion
}

type requestLogAttributes struct {
	mu          sync.RWMutex
	operation   string
	completions CompletionDetails
	attributes  []slog.Attr
}

func contextWithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDContextKey{}, requestID)
}

func contextWithRequestLogAttributes(ctx context.Context) (context.Context, *requestLogAttributes) {
	attributes := &requestLogAttributes{
		operation: "http.request",
		completions: CompletionDetails{
			Success:  Completion{Event: "http.request.completed", Message: "API request completed"},
			Rejected: Completion{Event: "http.request.rejected", Message: "API request rejected"},
			Failed:   Completion{Event: "http.request.failed", Message: "API request failed"},
		},
	}
	return context.WithValue(ctx, requestLogAttributesContextKey{}, attributes), attributes
}

// SetRequestOperation gives the generic request middleware a stable operation
// and plain completion messages selected by the domain that owns the route.
func SetRequestOperation(ctx context.Context, operation string, completions CompletionDetails) {
	requestAttributes, ok := ctx.Value(requestLogAttributesContextKey{}).(*requestLogAttributes)
	if !ok {
		return
	}
	requestAttributes.mu.Lock()
	defer requestAttributes.mu.Unlock()
	requestAttributes.operation = operation
	requestAttributes.completions = completions
}

// AddRequestLogAttributes adds safe domain context to the request's single
// completion event. Callers must not add secrets, credentials, or payloads.
func AddRequestLogAttributes(ctx context.Context, attributes ...slog.Attr) {
	requestAttributes, ok := ctx.Value(requestLogAttributesContextKey{}).(*requestLogAttributes)
	if !ok {
		return
	}
	requestAttributes.mu.Lock()
	defer requestAttributes.mu.Unlock()
	requestAttributes.attributes = append(requestAttributes.attributes, attributes...)
}

func (attributes *requestLogAttributes) snapshot() (string, CompletionDetails, []slog.Attr) {
	attributes.mu.RLock()
	defer attributes.mu.RUnlock()
	return attributes.operation, attributes.completions, append([]slog.Attr(nil), attributes.attributes...)
}

type correlationHandler struct {
	next slog.Handler
}

func newCorrelationHandler(next slog.Handler) slog.Handler {
	return &correlationHandler{next: next}
}

func (handler *correlationHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return handler.next.Enabled(ctx, level)
}

func (handler *correlationHandler) Handle(ctx context.Context, record slog.Record) error {
	if requestID, ok := ctx.Value(requestIDContextKey{}).(string); ok && requestID != "" {
		record.AddAttrs(slog.String("request_id", requestID))
	}
	spanContext := trace.SpanContextFromContext(ctx)
	if spanContext.IsValid() {
		record.AddAttrs(
			slog.String("trace_id", spanContext.TraceID().String()),
			slog.String("span_id", spanContext.SpanID().String()),
		)
	}
	return handler.next.Handle(ctx, record)
}

func (handler *correlationHandler) WithAttrs(attributes []slog.Attr) slog.Handler {
	return &correlationHandler{next: handler.next.WithAttrs(attributes)}
}

func (handler *correlationHandler) WithGroup(name string) slog.Handler {
	return &correlationHandler{next: handler.next.WithGroup(name)}
}
