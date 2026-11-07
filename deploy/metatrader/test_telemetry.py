from contextlib import redirect_stderr, redirect_stdout
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import io
import json
import logging
import threading
import time
import unittest
from unittest.mock import patch

from opentelemetry.proto.collector.logs.v1.logs_service_pb2 import ExportLogsServiceRequest
from opentelemetry.proto.collector.metrics.v1.metrics_service_pb2 import ExportMetricsServiceRequest
from opentelemetry.proto.collector.trace.v1.trace_service_pb2 import ExportTraceServiceRequest

from telemetry import Telemetry


class TelemetryTests(unittest.TestCase):
    def test_real_otlp_export_is_correlated_and_omits_private_values(self) -> None:
        requests: dict[str, list[bytes]] = {}

        class Receiver(BaseHTTPRequestHandler):
            def do_POST(self) -> None:
                requests.setdefault(self.path, []).append(self.rfile.read(int(self.headers["Content-Length"])))
                self.send_response(200)
                self.send_header("Content-Length", "0")
                self.end_headers()

            def log_message(self, _format: str, *args: object) -> None:
                pass

        server = ThreadingHTTPServer(("127.0.0.1", 0), Receiver)
        worker = threading.Thread(target=server.serve_forever, daemon=True)
        worker.start()
        telemetry = Telemetry("mt4", "serve", f"http://127.0.0.1:{server.server_port}")
        private = "fixture-password-not-for-telemetry"
        try:
            with telemetry.operation("source_read"):
                correlation = telemetry.event("connector.source.accepted", {"read_only_verified": True, "password": private})
                telemetry.accepted_records({"open_orders": 2})
            with self.assertRaises(ValueError):
                with telemetry.operation("source_read"):
                    raise ValueError(private)
            telemetry.close()
            self.assertEqual(set(requests), {"/v1/logs", "/v1/traces", "/v1/metrics"})
            for batches in requests.values():
                self.assertNotIn(private.encode(), b"".join(batches))
            traces = [ExportTraceServiceRequest.FromString(raw) for raw in requests["/v1/traces"]]
            spans = [span for request in traces for resource in request.resource_spans for scope in resource.scope_spans for span in scope.spans]
            self.assertEqual({span.name for span in spans}, {"connector.source_read"})
            self.assertTrue(all(not span.events for span in spans))
            self.assertIn(correlation["trace_id"], {span.trace_id.hex() for span in spans})
            logs = [ExportLogsServiceRequest.FromString(raw) for raw in requests["/v1/logs"]]
            records = [record for request in logs for resource in request.resource_logs for scope in resource.scope_logs for record in scope.log_records]
            accepted = next(record for record in records if record.body.string_value == "connector.source.accepted")
            self.assertEqual(accepted.trace_id.hex(), correlation["trace_id"])
            self.assertEqual(accepted.span_id.hex(), correlation["span_id"])
            metrics = [ExportMetricsServiceRequest.FromString(raw) for raw in requests["/v1/metrics"]]
            names = {metric.name for request in metrics for resource in request.resource_metrics for scope in resource.scope_metrics for metric in scope.metrics}
            self.assertTrue({"connector.operations", "connector.operation.duration", "connector.operation.active",
                             "connector.source.records", "connector.runtime.healthy"}.issubset(names))
        finally:
            telemetry.close()
            server.shutdown()
            server.server_close()
            worker.join(timeout=2)

    def test_unreachable_exporter_has_bounded_shutdown_and_safe_failure_output(self) -> None:
        output = io.StringIO()
        with redirect_stdout(output), redirect_stderr(output):
            telemetry = Telemetry("mt5", "probe", "http://127.0.0.1:1")
            with telemetry.operation("mt5_probe"):
                telemetry.event("connector.mt5.ipc_probe", {"initialized": False})
            logging.getLogger("opentelemetry.exporter").error("fixture-credential-must-not-leak")
            started = time.monotonic()
            telemetry.close()
            self.assertLess(time.monotonic() - started, 9)
        self.assertNotIn("fixture-credential-must-not-leak", output.getvalue())
        records = [json.loads(line) for line in output.getvalue().splitlines()]
        self.assertTrue(any(record["event"] == "connector.telemetry.export_failed" for record in records))

    def test_exporter_credentials_cannot_be_inherited_from_environment(self) -> None:
        with patch.dict("os.environ", {"OTEL_EXPORTER_OTLP_HEADERS": "api-key=fixture-private-key"}):
            with self.assertRaisesRegex(ValueError, "connector_telemetry_credentials_forbidden"):
                Telemetry("mt4", "collect", "http://127.0.0.1:4318")


if __name__ == "__main__":
    unittest.main()
