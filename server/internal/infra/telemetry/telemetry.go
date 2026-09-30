package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otellogglobal "go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

const (
	instrumentationName  = "github.com/Rahmannugar/macro-terminal/server/internal/infra/telemetry"
	metricExportInterval = 10 * time.Second
)

type Runtime struct {
	logger         *slog.Logger
	loggerProvider *log.LoggerProvider
	meterProvider  *metric.MeterProvider
	tracerProvider *trace.TracerProvider
	propagator     propagation.TextMapPropagator
}

func New(ctx context.Context, serviceName, environment string) (*Runtime, error) {
	if serviceName == "" {
		return nil, fmt.Errorf("telemetry service name must not be empty")
	}
	instanceID, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("create service instance ID: %w", err)
	}
	serviceResource, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(serviceName),
			semconv.ServiceInstanceID(instanceID.String()),
			semconv.DeploymentEnvironmentNameKey.String(environment),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create telemetry resource: %w", err)
	}

	traceExporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("create OTLP trace exporter: %w", err)
	}
	tracerProvider := trace.NewTracerProvider(
		trace.WithResource(serviceResource),
		trace.WithSampler(healthSuppressingSampler{
			delegate: trace.ParentBased(trace.AlwaysSample()),
		}),
		trace.WithBatcher(traceExporter),
	)

	metricExporter, err := otlpmetrichttp.New(ctx)
	if err != nil {
		_ = tracerProvider.Shutdown(ctx)
		return nil, fmt.Errorf("create OTLP metric exporter: %w", err)
	}
	meterProvider := metric.NewMeterProvider(
		metric.WithResource(serviceResource),
		metric.WithReader(metric.NewPeriodicReader(
			metricExporter,
			metric.WithInterval(metricExportInterval),
		)),
	)

	logExporter, err := otlploghttp.New(ctx)
	if err != nil {
		_ = meterProvider.Shutdown(ctx)
		_ = tracerProvider.Shutdown(ctx)
		return nil, fmt.Errorf("create OTLP log exporter: %w", err)
	}
	loggerProvider := log.NewLoggerProvider(
		log.WithResource(serviceResource),
		log.WithProcessor(log.NewBatchProcessor(logExporter)),
	)

	propagator := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
	otel.SetTracerProvider(tracerProvider)
	otel.SetMeterProvider(meterProvider)
	otel.SetTextMapPropagator(propagator)
	otellogglobal.SetLoggerProvider(loggerProvider)

	stdoutHandler := slog.NewJSONHandler(os.Stdout, nil)
	otelHandler := otelslog.NewHandler(
		instrumentationName,
		otelslog.WithLoggerProvider(loggerProvider),
	)
	logger := slog.New(newCorrelationHandler(slog.NewMultiHandler(stdoutHandler, otelHandler))).With(
		"service.name", serviceName,
		"service.instance.id", instanceID.String(),
		"deployment.environment.name", environment,
	)

	return &Runtime{
		logger:         logger,
		loggerProvider: loggerProvider,
		meterProvider:  meterProvider,
		tracerProvider: tracerProvider,
		propagator:     propagator,
	}, nil
}

func (runtime *Runtime) Logger() *slog.Logger {
	return runtime.logger
}

func (runtime *Runtime) Shutdown(ctx context.Context) error {
	return errors.Join(
		runtime.loggerProvider.Shutdown(ctx),
		runtime.meterProvider.Shutdown(ctx),
		runtime.tracerProvider.Shutdown(ctx),
	)
}

type healthSuppressingSampler struct {
	delegate trace.Sampler
}

func (sampler healthSuppressingSampler) ShouldSample(parameters trace.SamplingParameters) trace.SamplingResult {
	for _, current := range parameters.Attributes {
		if current.Key == healthProbeAttribute && current.Value.AsBool() {
			return trace.SamplingResult{Decision: trace.Drop}
		}
	}
	return sampler.delegate.ShouldSample(parameters)
}

func (sampler healthSuppressingSampler) Description() string {
	return "SuppressSuccessfulHealthProbes{" + sampler.delegate.Description() + "}"
}
