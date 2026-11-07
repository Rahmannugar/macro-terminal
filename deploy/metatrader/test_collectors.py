from collections import namedtuple
import importlib.util
from pathlib import Path
import time
import unittest

from account_request import MAX_WINDOW_SECONDS, RequestFailure, validate_request


SPEC = importlib.util.spec_from_file_location("mt5_collect", Path(__file__).with_name("mt5_collect.py"))
assert SPEC is not None and SPEC.loader is not None
collector = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(collector)


class SourceReadTests(unittest.TestCase):
    def test_source_identifiers_and_binary64_values_survive_encoding(self) -> None:
        record = namedtuple("Deal", "ticket profit entry symbol")(2**63 + 3, 0.10000000000000002, 1, "EURUSD")
        encoded = collector.encode(record)
        self.assertEqual(encoded["ticket"], "9223372036854775811")
        self.assertEqual(float(encoded["profit"]), record.profit)
        self.assertEqual(encoded["entry"], "1")

    def test_failed_or_incomplete_reads_do_not_become_empty_snapshots(self) -> None:
        for records, count, error in [(None, 0, "source_read_failed"), ((), 1, "source_snapshot_changed")]:
            with self.subTest(error=error):
                with self.assertRaisesRegex(collector.ReadFailure, error):
                    collector.checked_records(records, count)
        self.assertEqual(collector.checked_records((), 0), [])

    def test_unbounded_history_window_is_rejected_before_adapter_access(self) -> None:
        end = int(time.time())
        request = {"login": "123", "server": "private-fixture", "password": "private-fixture",
                   "from_epoch": str(end - MAX_WINDOW_SECONDS - 1), "to_epoch": str(end)}
        with self.assertRaisesRegex(RequestFailure, "invalid_history_window"):
            validate_request(request)

    def test_mt4_configuration_injection_and_noncanonical_logins_are_rejected(self) -> None:
        end = int(time.time())
        request = {"login": "123", "server": "fixture-server", "password": "fixture-password", "symbol": "EURUSD",
                   "from_epoch": str(end - 3600), "to_epoch": str(end)}
        for key, value in [("login", "١٢٣"), ("server", "../fixture"), ("password", "fixture\nExpertsTrades=true")]:
            with self.subTest(key=key), self.assertRaisesRegex(RequestFailure, "invalid_request"):
                validate_request({**request, key: value}, "mt4")

    def test_mt4_text_configuration_rejects_unverified_encoding_without_restricting_mt5(self) -> None:
        end = int(time.time())
        request = {"login": "123", "server": "fixture-server", "password": "fixture-password", "symbol": "EURUSD",
                   "from_epoch": str(end - 3600), "to_epoch": str(end)}
        for key in ("server", "password", "symbol"):
            with self.subTest(key=key), self.assertRaisesRegex(RequestFailure, "invalid_request"):
                validate_request({**request, key: request[key] + "é"}, "mt4")
        request.pop("symbol")
        request.update(server="fixture-é", password="fixture-é")
        validated = validate_request(request, "mt5")
        self.assertEqual(validated["server"], request["server"])
        self.assertEqual(validated["password"], request["password"])

    def test_trade_disabled_flag_does_not_override_wrong_identity_or_disconnection(self) -> None:
        account = namedtuple("Account", "login server trade_allowed")(123, "fixture-server", False)
        terminal = namedtuple("Terminal", "connected")(True)
        with self.assertRaisesRegex(collector.ReadFailure, "account_identity_mismatch"):
            collector.verify_identity(account, terminal, 456, "fixture-server")
        with self.assertRaisesRegex(collector.ReadFailure, "broker_disconnected"):
            collector.verify_identity(account, terminal._replace(connected=False), 123, "fixture-server")
        with self.assertRaisesRegex(collector.ReadFailure, "read_only_not_verified"):
            collector.verify_identity(account._replace(trade_allowed=True), terminal, 123, "fixture-server")

    def test_nonfinite_source_number_is_rejected(self) -> None:
        record_type = namedtuple("Deal", "profit")
        for value in [float("nan"), float("inf"), float("-inf")]:
            with self.subTest(value=value):
                with self.assertRaisesRegex(collector.ReadFailure, "invalid_source_number"):
                    collector.encode(record_type(value))


if __name__ == "__main__":
    unittest.main()
