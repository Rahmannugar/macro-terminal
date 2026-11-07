from __future__ import annotations

from collections.abc import Iterator
from contextlib import contextmanager
import json
import logging
import os
import re
import threading
import time
from urllib.parse import urlsplit
import uuid
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from opentelemetry.metrics import Observation
    from opentelemetry.sdk._logs import LoggerProvider
    from opentelemetry.sdk.metrics import MeterProvider
    from opentelemetry.sdk.trace import TracerProvider


Attribute = str | int | bool
SERVICE_NAME = "macro-terminal-connectivity"
SAFE_ATTRIBUTES = {
    "artifact", "bytes", "acquisition", "http_status", "collector", "account_configured",
    "python", "python_bits", "numpy", "metatrader5", "initialized", "error_code",
    "broker_connectivity_verified", "terminal_version", "terminal_build", "outcome",
    "orders", "positions", "open_orders", "history_orders", "history_deals",
    "read_only_verified", "history_coverage", "operation", "command",
    "installer_exit", "collector_install_verified", "webview2_version", "installer_timed_out", "browser_execution_verified",
}


class ExportFailureHandler(logging.Handler):
    def __init__(self, platform: str):
        super().__init__(logging.WARNING)
        self.platform = platform
        self.last_reported = float("-inf")

    def emit(self, record: logging.LogRecord) -> None:
        observed = time.monotonic()
        if observed - self.last_reported >= 60:
            self.last_reported = observed
            print(json.dumps({"event": "connector.telemetry.export_failed", "service.name": SERVICE_NAME,
                              "platform": self.platform, "error_code": "telemetry_export_failed"}), flush=True)


def safe_attributes(attributes: dict[str, Attribute]) -> dict[str, Attribute]:
    return {key: value for key, value in attributes.items()
            if key in SAFE_ATTRIBUTES and isinstance(value, (str, int, bool))
            and (not isinstance(value, str) or len(value) <= 160)}


class Telemetry:
    def __init__(self, platform: str, command: str, endpoint: str):
        from opentelemetry.exporter.otlp.proto.http._log_exporter import OTLPLogExporter
        from opentelemetry.exporter.otlp.proto.http.metric_exporter import OTLPMetricExporter
        from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
        from opentelemetry.sdk._logs import LoggerProvider
        from opentelemetry.sdk._logs.export import BatchLogRecordProcessor
        from opentelemetry.sdk.metrics import MeterProvider
        from opentelemetry.sdk.metrics.export import PeriodicExportingMetricReader
        from opentelemetry.sdk.resources import Resource
        from opentelemetry.sdk.trace import TracerProvider
        from opentelemetry.sdk.trace.export import BatchSpanProcessor

        parsed = urlsplit(endpoint)
        if parsed.scheme not in ("http", "https") or not parsed.hostname or parsed.username or parsed.password:
            raise ValueError("invalid_telemetry_endpoint")
        if parsed.query or parsed.fragment or parsed.path not in ("", "/"):
            raise ValueError("invalid_telemetry_endpoint")
        if any(name.startswith("OTEL_") and (name.endswith("_HEADERS") or "CREDENTIAL_PROVIDER" in name)
               and os.environ[name] for name in os.environ):
            raise ValueError("connector_telemetry_credentials_forbidden")
        handler = ExportFailureHandler(platform)
        for name in ("opentelemetry", "urllib3", "requests"):
            logger = logging.getLogger(name)
            logger.handlers = [handler]
            logger.propagate = False
        self.platform = platform
        self.instance_id = str(uuid.uuid4())
        resource = Resource({
            "service.name": SERVICE_NAME, "service.instance.id": self.instance_id,
            "service.version": "0.1.0", "deployment.environment.name": "connectivity",
            "connector.platform": platform, "connector.command": command,
        })
        endpoint = endpoint.rstrip("/")
        self.closed = False
        self.traces = TracerProvider(resource=resource, shutdown_on_exit=False)
        self.traces.add_span_processor(BatchSpanProcessor(
            OTLPSpanExporter(endpoint=endpoint + "/v1/traces", timeout=2),
            max_queue_size=128, max_export_batch_size=32, schedule_delay_millis=1000,
        ))
        self.metrics = MeterProvider(resource=resource, shutdown_on_exit=False, metric_readers=[
            PeriodicExportingMetricReader(
                OTLPMetricExporter(endpoint=endpoint + "/v1/metrics", timeout=2),
                export_interval_millis=10000, export_timeout_millis=3000,
            ),
        ])
        self.logs = LoggerProvider(resource=resource, shutdown_on_exit=False)
        self.logs.add_log_record_processor(BatchLogRecordProcessor(
            OTLPLogExporter(endpoint=endpoint + "/v1/logs", timeout=2),
            max_queue_size=128, max_export_batch_size=32, schedule_delay_millis=1000,
        ))
        self.logger = self.logs.get_logger("macro-terminal.connector")
        self.tracer = self.traces.get_tracer("macro-terminal.connector")
        meter = self.metrics.get_meter("macro-terminal.connector")
        self.operations = meter.create_counter("connector.operations", unit="{operation}")
        self.duration = meter.create_histogram("connector.operation.duration", unit="s")
        self.active = meter.create_up_down_counter("connector.operation.active", unit="{operation}")
        self.records = meter.create_counter("connector.source.records", unit="{record}")
        self.runtime_healthy = 0
        if command == "serve":
            meter.create_observable_gauge(
                "connector.runtime.healthy", callbacks=[self._health], unit="1",
            )

    def _health(self, _options: object) -> Iterator[Observation]:
        from opentelemetry.metrics import Observation
        yield Observation(self.runtime_healthy, {"platform": self.platform})

    def event(self, name: str, attributes: dict[str, Attribute]) -> dict[str, Attribute]:
        from opentelemetry import trace
        from opentelemetry._logs import SeverityNumber

        attributes = safe_attributes(attributes)
        context = trace.get_current_span().get_span_context()
        failed = name.endswith("failed") or attributes.get("outcome") == "failed"
        self.logger.emit(
            body=name, attributes={"event": name, "platform": self.platform, **attributes},
            severity_number=SeverityNumber.ERROR if failed else SeverityNumber.INFO,
            severity_text="ERROR" if failed else "INFO",
        )
        correlation: dict[str, Attribute] = {"service.instance.id": self.instance_id}
        if context.is_valid:
            correlation["trace_id"] = format(context.trace_id, "032x")
            correlation["span_id"] = format(context.span_id, "016x")
        return correlation

    @contextmanager
    def operation(self, name: str) -> Iterator[None]:
        from opentelemetry.trace import Status, StatusCode

        labels: dict[str, Attribute] = {"platform": self.platform, "operation": name}
        started = time.monotonic()
        outcome = "succeeded"
        self.active.add(1, labels)
        with self.tracer.start_as_current_span("connector." + name, attributes=labels,
                                               record_exception=False, set_status_on_exception=False) as span:
            try:
                yield
            except BaseException as error:
                outcome = "cancelled" if isinstance(error, KeyboardInterrupt) else "failed"
                code = getattr(error, "code", "runtime_operation_failed")
                if not isinstance(code, str) or not re.fullmatch(r"[a-z][a-z0-9_]{0,79}", code):
                    code = "runtime_operation_failed"
                span.set_attribute("error_code", code)
                span.set_status(Status(StatusCode.ERROR))
                self.event("connector.operation.failed", {"operation": name, "outcome": outcome, "error_code": code})
                raise
            finally:
                self.active.add(-1, labels)
                self.operations.add(1, {**labels, "outcome": outcome})
                self.duration.record(time.monotonic() - started, {**labels, "outcome": outcome})

    def accepted_records(self, counts: dict[str, int]) -> None:
        for kind, count in counts.items():
            if kind in ("open_orders", "orders", "positions", "history_orders", "history_deals") and 0 <= count <= 10000:
                self.records.add(count, {"platform": self.platform, "source_kind": kind})

    def close(self) -> None:
        from opentelemetry.sdk.metrics import MeterProvider

        if self.closed:
            return
        self.closed = True
        failures: list[bool] = []

        def close_provider(provider: LoggerProvider | MeterProvider | TracerProvider) -> None:
            try:
                if not provider.force_flush(timeout_millis=3000):
                    failures.append(True)
                if isinstance(provider, MeterProvider):
                    provider.shutdown(timeout_millis=3000)
                else:
                    provider.shutdown()
            except Exception:
                failures.append(True)

        deadline = time.monotonic() + 8
        workers = [threading.Thread(target=close_provider, args=(provider,), daemon=True)
                   for provider in (self.logs, self.metrics, self.traces)]
        for worker in workers:
            worker.start()
        for worker in workers:
            worker.join(max(0, deadline - time.monotonic()))
        if failures or any(worker.is_alive() for worker in workers):
            print(json.dumps({"event": "connector.telemetry.shutdown_failed",
                              "service.name": SERVICE_NAME, "platform": self.platform}), flush=True)
