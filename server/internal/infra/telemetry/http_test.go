package telemetry

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestHTTPMiddlewareExcludesStreamAndEmitsStandardDuration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	defer func() { _ = meterProvider.Shutdown(context.Background()) }()
	tracerProvider := sdktrace.NewTracerProvider()
	defer func() { _ = tracerProvider.Shutdown(context.Background()) }()
	var logs bytes.Buffer
	runtime := &Runtime{
		logger:         slog.New(slog.NewJSONHandler(&logs, nil)),
		meterProvider:  meterProvider,
		tracerProvider: tracerProvider,
		propagator:     propagation.TraceContext{},
	}
	middleware, err := runtime.HTTPMiddleware()
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.Use(middleware)
	router.GET("/api/v1/articles", func(ctx *gin.Context) { ctx.Status(http.StatusOK) })
	router.GET("/api/v1/events", func(ctx *gin.Context) {
		if ctx.Query("deny") != "" {
			ctx.Status(http.StatusUnauthorized)
			return
		}
		ctx.Status(http.StatusOK)
	})
	for _, path := range []string{"/api/v1/articles", "/api/v1/events"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("%s returned %d", path, response.Code)
		}
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/events?deny=1", nil))
	if response.Code != http.StatusUnauthorized || !strings.Contains(logs.String(), `"event":"http.request.rejected"`) {
		t.Fatalf("rejected stream response = %d, log = %s", response.Code, logs.String())
	}
	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatal(err)
	}
	for _, scope := range metrics.ScopeMetrics {
		for _, item := range scope.Metrics {
			if item.Name != "http.server.request.duration" {
				continue
			}
			histogram, ok := item.Data.(metricdata.Histogram[float64])
			if !ok || len(histogram.DataPoints) != 1 || histogram.DataPoints[0].Count != 1 {
				t.Fatalf("standard duration data = %#v", item.Data)
			}
			attributes := histogram.DataPoints[0].Attributes
			if route, ok := attributes.Value("http.route"); !ok || route.AsString() != "/api/v1/articles" {
				t.Fatalf("recorded route = %v", route)
			}
			return
		}
	}
	t.Fatal("standard HTTP duration metric missing")
}
