package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/infra/safehttp"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const (
	healthProbeAttribute = attribute.Key("macro_terminal.health_probe")
	requestIDHeader      = "X-Request-ID"
)

var fallbackRequestID atomic.Uint64

type httpInstruments struct {
	duration metric.Float64Histogram
	requests metric.Int64Counter
	errors   metric.Int64Counter
	active   metric.Int64UpDownCounter
}

func (runtime *Runtime) HTTPMiddleware() (gin.HandlerFunc, error) {
	meter := runtime.meterProvider.Meter(instrumentationName)
	duration, err := meter.Float64Histogram(
		"macro_terminal.http.server.request.duration",
		metric.WithDescription("Duration of completed Macro Terminal API requests."),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("create HTTP duration metric: %w", err)
	}
	requests, err := meter.Int64Counter(
		"macro_terminal.http.server.requests",
		metric.WithDescription("Number of completed Macro Terminal API requests."),
	)
	if err != nil {
		return nil, fmt.Errorf("create HTTP request metric: %w", err)
	}
	errors, err := meter.Int64Counter(
		"macro_terminal.http.server.errors",
		metric.WithDescription("Number of completed Macro Terminal API requests with server errors."),
	)
	if err != nil {
		return nil, fmt.Errorf("create HTTP error metric: %w", err)
	}
	active, err := meter.Int64UpDownCounter(
		"macro_terminal.http.server.active_requests",
		metric.WithDescription("Number of Macro Terminal API requests currently in progress."),
	)
	if err != nil {
		return nil, fmt.Errorf("create HTTP active-request metric: %w", err)
	}

	instruments := httpInstruments{
		duration: duration,
		requests: requests,
		errors:   errors,
		active:   active,
	}
	tracer := runtime.tracerProvider.Tracer(instrumentationName)

	return func(ginContext *gin.Context) {
		startedAt := time.Now()
		requestID := resolveRequestID(ginContext.GetHeader(requestIDHeader))
		ginContext.Header(requestIDHeader, requestID)

		route := ginContext.FullPath()
		if route == "" {
			route = "unmatched"
		}
		healthRequest := strings.HasPrefix(route, "/health/")
		operation := "http.request"
		if healthRequest {
			operation = healthOperation(route)
		}

		parentContext := runtime.propagator.Extract(
			ginContext.Request.Context(),
			propagation.HeaderCarrier(ginContext.Request.Header),
		)
		traceAttributes := []attribute.KeyValue{
			attribute.String("http.request.method", ginContext.Request.Method),
			attribute.String("http.route", route),
			attribute.String("http.request_id", requestID),
			attribute.String("macro_terminal.operation", operation),
			healthProbeAttribute.Bool(healthRequest),
		}
		requestContext, span := tracer.Start(
			parentContext,
			operation,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(traceAttributes...),
		)
		requestContext = contextWithRequestID(requestContext, requestID)
		requestContext, requestLogAttributes := contextWithRequestLogAttributes(requestContext)
		ginContext.Request = ginContext.Request.WithContext(requestContext)

		if !healthRequest {
			instruments.active.Add(requestContext, 1)
		}

		ginContext.Next()

		status := ginContext.Writer.Status()
		duration := time.Since(startedAt)
		outcome, errorCategory := classifyHTTPStatus(status)
		var completions CompletionDetails
		var domainAttributes []slog.Attr
		if !healthRequest {
			operation, completions, domainAttributes = requestLogAttributes.snapshot()
			span.SetName(operation)
		}
		span.SetAttributes(
			attribute.Int("http.response.status_code", status),
			attribute.String("macro_terminal.operation", operation),
			attribute.String("macro_terminal.outcome", outcome),
			attribute.String("macro_terminal.error_category", errorCategory),
		)
		if status >= http.StatusInternalServerError {
			span.SetStatus(codes.Error, errorCategory)
		}
		span.End()

		if healthRequest {
			if route == "/health/ready" && status >= http.StatusInternalServerError {
				runtime.recordReadinessFailure(
					startedAt,
					duration,
					status,
					requestID,
				)
			}
			return
		}

		metricAttributes := metric.WithAttributes(
			attribute.String("operation", operation),
			attribute.String("method", ginContext.Request.Method),
			attribute.String("route", route),
			attribute.String("status_class", statusClass(status)),
			attribute.String("outcome", outcome),
			attribute.String("error_category", errorCategory),
		)
		instruments.active.Add(requestContext, -1)
		instruments.requests.Add(requestContext, 1, metricAttributes)
		instruments.duration.Record(requestContext, duration.Seconds(), metricAttributes)
		if status >= http.StatusInternalServerError {
			instruments.errors.Add(requestContext, 1, metricAttributes)
		}

		logRequestCompletion(
			runtime.logger,
			requestContext,
			operation,
			ginContext.Request.Method,
			route,
			status,
			duration,
			outcome,
			errorCategory,
			completions,
			domainAttributes,
		)
	}, nil
}

func (runtime *Runtime) recordReadinessFailure(
	startedAt time.Time,
	duration time.Duration,
	status int,
	requestID string,
) {
	failureContext := contextWithRequestID(context.Background(), requestID)
	failureContext, span := runtime.tracerProvider.Tracer(instrumentationName).Start(
		failureContext,
		"health.readiness.check",
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithTimestamp(startedAt),
		trace.WithAttributes(
			attribute.String("http.request.method", http.MethodGet),
			attribute.String("http.route", "/health/ready"),
			attribute.String("http.request_id", requestID),
			attribute.Int("http.response.status_code", status),
			attribute.String("macro_terminal.operation", "health.readiness.check"),
			attribute.String("macro_terminal.outcome", "error"),
			attribute.String("macro_terminal.error_category", "dependency_unavailable"),
		),
	)
	span.SetStatus(codes.Error, "dependency_unavailable")
	runtime.logger.ErrorContext(failureContext, "API is not ready",
		"event", "health.readiness.failed",
		"operation", "health.readiness.check",
		"method", http.MethodGet,
		"route", "/health/ready",
		"status", status,
		"duration_ms", float64(duration.Microseconds())/1000,
		"outcome", "error",
		"error_category", "dependency_unavailable",
	)
	span.End(trace.WithTimestamp(startedAt.Add(duration)))
}

func NewHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Transport: otelhttp.NewTransport(safehttp.Transport()),
		Timeout:   timeout,
	}
}

func resolveRequestID(candidate string) string {
	candidate = strings.TrimSpace(candidate)
	if len(candidate) > 0 && len(candidate) <= 128 {
		valid := true
		for _, current := range candidate {
			if (current >= 'a' && current <= 'z') ||
				(current >= 'A' && current <= 'Z') ||
				(current >= '0' && current <= '9') ||
				strings.ContainsRune("._:-", current) {
				continue
			}
			valid = false
			break
		}
		if valid {
			return candidate
		}
	}

	generated, err := uuid.NewV7()
	if err == nil {
		return generated.String()
	}
	return fmt.Sprintf("fallback-%d-%d", time.Now().UnixNano(), fallbackRequestID.Add(1))
}

func healthOperation(route string) string {
	if route == "/health/ready" {
		return "health.readiness"
	}
	return "health.liveness"
}

func classifyHTTPStatus(status int) (string, string) {
	switch {
	case status < 400:
		return "success", "none"
	case status == http.StatusUnauthorized:
		return "rejected", "unauthenticated"
	case status == http.StatusForbidden:
		return "rejected", "forbidden"
	case status == http.StatusNotFound:
		return "rejected", "not_found"
	case status == http.StatusConflict:
		return "rejected", "conflict"
	case status == http.StatusTooManyRequests:
		return "rejected", "rate_limited"
	case status < 500:
		return "rejected", "invalid_request"
	default:
		return "error", "server_error"
	}
}

func statusClass(status int) string {
	return fmt.Sprintf("%dxx", status/100)
}

func logRequestCompletion(
	logger *slog.Logger,
	ctx context.Context,
	operation string,
	method string,
	route string,
	status int,
	duration time.Duration,
	outcome string,
	errorCategory string,
	completions CompletionDetails,
	requestAttributes []slog.Attr,
) {
	completion := completionForOutcome(completions, outcome)
	attributes := []slog.Attr{
		slog.String("event", completion.Event),
		slog.String("operation", operation),
		slog.String("method", method),
		slog.String("route", route),
		slog.Int("status", status),
		slog.Float64("duration_ms", float64(duration.Microseconds())/1000),
		slog.String("outcome", outcome),
	}
	if errorCategory != "none" {
		attributes = append(attributes, slog.String("error_category", errorCategory))
	}
	attributes = append(attributes, requestAttributes...)
	level := slog.LevelInfo
	if status >= http.StatusInternalServerError {
		level = slog.LevelError
	}
	logger.LogAttrs(ctx, level, completion.Message, attributes...)
}

func completionForOutcome(completions CompletionDetails, outcome string) Completion {
	switch outcome {
	case "success":
		return completions.Success
	case "rejected":
		return completions.Rejected
	default:
		return completions.Failed
	}
}
