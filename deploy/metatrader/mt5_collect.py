import argparse
from datetime import datetime, timezone
import json
import math
from pathlib import Path
import sys
from typing import Protocol

from account_request import AccountRequest, RequestFailure, validate_request


MAX_RECORDS = 10_000
MAX_OUTPUT_BYTES = 16 * 1024 * 1024


class SourceRecord(Protocol):
    def _asdict(self) -> dict[str, str | int | float | bool]: ...


class ReadFailure(Exception):
    def __init__(self, code: str):
        self.code = code
        super().__init__(code)


def encode(record: SourceRecord) -> dict[str, str | bool]:
    result: dict[str, str | bool] = {}
    for field, value in record._asdict().items():
        if isinstance(value, bool):
            result[field] = value
        elif isinstance(value, int):
            result[field] = str(value)
        elif isinstance(value, float):
            if not math.isfinite(value):
                raise ReadFailure("invalid_source_number")
            result[field] = repr(value)
        elif isinstance(value, str):
            result[field] = value
        else:
            raise ReadFailure("invalid_source_field")
    return result


def checked_records(records: tuple[SourceRecord, ...] | None, expected: int | None) -> list[dict[str, str | bool]]:
    if records is None:
        raise ReadFailure("source_read_failed")
    if len(records) > MAX_RECORDS:
        raise ReadFailure("record_limit_exceeded")
    if expected is not None and len(records) != expected:
        raise ReadFailure("source_snapshot_changed")
    return [encode(record) for record in records]


def verify_identity(account: SourceRecord | None, terminal: SourceRecord | None, login: int, server: str) -> None:
    if account is None or terminal is None:
        raise ReadFailure("account_unavailable")
    account_fields = account._asdict()
    terminal_fields = terminal._asdict()
    if not terminal_fields.get("connected"):
        raise ReadFailure("broker_disconnected")
    if account_fields.get("login") != login or account_fields.get("server") != server:
        raise ReadFailure("account_identity_mismatch")
    if account_fields.get("trade_allowed") is not False:
        raise ReadFailure("read_only_not_verified")


def collect(request: AccountRequest, output: Path, attempt: str) -> dict[str, int]:
    request = validate_request(request)
    login, start, end = (int(request[key]) for key in ("login", "from_epoch", "to_epoch"))
    import MetaTrader5 as mt5

    try:
        if not mt5.initialize(
            "C:\\Terminal\\terminal64.exe", login=login, server=request["server"],
            password=request["password"], timeout=60000, portable=True,
        ):
            raise ReadFailure("terminal_authentication_failed")
        if not mt5.login(login, password=request["password"], server=request["server"], timeout=30000):
            raise ReadFailure("terminal_authentication_failed")
        account = mt5.account_info()
        terminal = mt5.terminal_info()
        verify_identity(account, terminal, login, request["server"])
        assert account is not None and terminal is not None
        from_date = datetime.fromtimestamp(start, timezone.utc)
        to_date = datetime.fromtimestamp(end, timezone.utc)
        current_order_count = mt5.orders_total()
        current_position_count = mt5.positions_total()
        history_order_count = mt5.history_orders_total(from_date, to_date)
        history_deal_count = mt5.history_deals_total(from_date, to_date)
        counts = [current_order_count, current_position_count, history_order_count, history_deal_count]
        if any(not isinstance(count, int) or count < 0 for count in counts):
            raise ReadFailure("source_count_failed")
        if any(count > MAX_RECORDS for count in counts):
            raise ReadFailure("record_limit_exceeded")
        orders = checked_records(mt5.orders_get(), current_order_count)
        positions = checked_records(mt5.positions_get(), current_position_count)
        history_orders = checked_records(mt5.history_orders_get(from_date, to_date), history_order_count)
        history_deals = checked_records(mt5.history_deals_get(from_date, to_date), history_deal_count)
        verify_identity(mt5.account_info(), mt5.terminal_info(), login, request["server"])
        if mt5.orders_total() != current_order_count or mt5.positions_total() != current_position_count:
            raise ReadFailure("source_snapshot_changed")
        if mt5.history_orders_total(from_date, to_date) != history_order_count:
            raise ReadFailure("source_snapshot_changed")
        if mt5.history_deals_total(from_date, to_date) != history_deal_count:
            raise ReadFailure("source_snapshot_changed")
        snapshot = {
            "schema_version": 1,
            "platform": "mt5",
            "attempt_id": attempt,
            "observed_at": datetime.now(timezone.utc).isoformat(),
            "account": {key: value for key, value in encode(account).items() if key != "name"},
            "terminal_build": str(terminal._asdict()["build"]),
            "read_only_verified": False,
            "read_only_evidence": "connected_identity_and_trade_disabled_flag_only",
            "source_numbers": "decimal_strings_from_platform_binary64_and_integer_values",
            "history_coverage": {
                "from_epoch": str(start), "to_epoch": str(end),
                "status": "broker_returned_window", "full_account_history_verified": False,
            },
            "snapshot_consistency": "sequential_reads_with_identity_and_count_rechecks_not_transactional",
            "orders": orders,
            "positions": positions,
            "history_orders": history_orders,
            "history_deals": history_deals,
        }
        encoded = json.dumps(snapshot, ensure_ascii=False, allow_nan=False).encode("utf-8")
        if len(encoded) > MAX_OUTPUT_BYTES:
            raise ReadFailure("output_limit_exceeded")
        temporary = output.with_suffix(".partial")
        temporary.write_bytes(encoded)
        temporary.replace(output)
        return {
            "orders": len(orders), "positions": len(positions),
            "history_orders": len(history_orders), "history_deals": len(history_deals),
        }
    finally:
        mt5.shutdown()


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--request", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--attempt", required=True)
    arguments = parser.parse_args()
    arguments.output.unlink(missing_ok=True)
    try:
        if arguments.request.stat().st_size > 16 * 1024:
            raise ReadFailure("invalid_request")
        request = json.loads(arguments.request.read_text())
        if len(arguments.attempt) != 32 or any(character not in "0123456789abcdef" for character in arguments.attempt):
            raise ReadFailure("invalid_request")
        counts = collect(request, arguments.output, arguments.attempt)
        print(json.dumps({"event": "connector.mt5.source_read", "outcome": "read", **counts}), flush=True)
        return 0
    except (ReadFailure, RequestFailure) as error:
        print(json.dumps({"event": "connector.mt5.source_read", "outcome": "failed", "error_code": error.code}), flush=True)
        return 1
    except Exception:
        print(json.dumps({"event": "connector.mt5.source_read", "outcome": "failed", "error_code": "adapter_operation_failed"}), flush=True)
        return 1
    finally:
        arguments.output.with_suffix(".partial").unlink(missing_ok=True)


if __name__ == "__main__":
    sys.exit(main())
