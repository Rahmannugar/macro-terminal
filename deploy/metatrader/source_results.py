import json
import os
from pathlib import Path
import re
import stat
from typing import TypeAlias

from account_request import AccountRequest


JSONValue: TypeAlias = str | int | float | bool | None | list["JSONValue"] | dict[str, "JSONValue"]
JournalCursor: TypeAlias = dict[Path, tuple[int, int]]
MAX_OUTPUT_BYTES = 16 * 1024 * 1024
MAX_JOURNAL_BYTES = 1024 * 1024
MAX_RECORDS = 10_000
SOURCE_KINDS = {
    "mt4": ("open_orders", "history_orders"),
    "mt5": ("orders", "positions", "history_orders", "history_deals"),
}


class ResultFailure(Exception):
    def __init__(self, code: str):
        self.code = code
        super().__init__(code)


def journal_cursor(directory: Path) -> JournalCursor:
    return {path: (path.stat().st_ino, path.stat().st_size) for path in directory.glob("*.log")}


def investor_authorized(directory: Path, login: str, cursor: JournalCursor) -> bool:
    pattern = re.compile(
        rf"(?:^|[\t ])['\"]?{re.escape(login)}['\"]?:[\t ]*"
        r"trading has been disabled[\t ]*-[\t ]*investor mode\b",
        re.IGNORECASE | re.MULTILINE,
    )
    read_bytes = 0
    for path in sorted(directory.glob("*.log")):
        if path.is_symlink():
            raise ResultFailure("investor_journal_changed")
        with path.open("rb") as source:
            metadata = os.fstat(source.fileno())
            previous = cursor.get(path)
            if previous is not None and previous[0] != metadata.st_ino:
                raise ResultFailure("investor_journal_changed")
            offset = previous[1] if previous is not None else 0
            if metadata.st_size < offset:
                raise ResultFailure("investor_journal_changed")
            header = source.read(2)
            source.seek(offset)
            data = source.read(MAX_JOURNAL_BYTES - read_bytes + 1)
        read_bytes += len(data)
        if read_bytes > MAX_JOURNAL_BYTES:
            raise ResultFailure("investor_journal_limit_exceeded")
        encoding = "utf-16-le" if header == b"\xff\xfe" else "utf-8"
        if pattern.search(data.decode(encoding, errors="replace")):
            return True
    return False


def reject_constant(_value: str) -> None:
    raise ResultFailure("invalid_source_output")


def read_source(path: Path, platform: str, request: AccountRequest, attempt: str) -> dict[str, JSONValue]:
    if path.is_symlink():
        raise ResultFailure("invalid_source_output")
    with path.open("rb") as source:
        if not stat.S_ISREG(os.fstat(source.fileno()).st_mode):
            raise ResultFailure("invalid_source_output")
        raw = source.read(MAX_OUTPUT_BYTES + 1)
    if len(raw) > MAX_OUTPUT_BYTES:
        raise ResultFailure("output_limit_exceeded")
    try:
        result: dict[str, JSONValue] = json.loads(raw, parse_constant=reject_constant)
    except (ValueError, UnicodeError) as error:
        raise ResultFailure("invalid_source_output") from error
    if not isinstance(result, dict) or result.get("schema_version") != 1 or result.get("platform") != platform:
        raise ResultFailure("invalid_source_output")
    if result.get("attempt_id") != attempt:
        raise ResultFailure("source_attempt_mismatch")
    account = result.get("account")
    if not isinstance(account, dict) or account.get("login") != request["login"] or account.get("server") != request["server"]:
        raise ResultFailure("account_identity_mismatch")
    if account.get("trade_allowed") is not False or result.get("read_only_verified") is not False:
        raise ResultFailure("read_only_not_verified")
    coverage = result.get("history_coverage")
    expected_status = "terminal_loaded_only" if platform == "mt4" else "broker_returned_window"
    if (not isinstance(coverage, dict) or coverage.get("status") != expected_status
            or coverage.get("from_epoch") != request["from_epoch"] or coverage.get("to_epoch") != request["to_epoch"]
            or coverage.get("full_account_history_verified") is not False):
        raise ResultFailure("invalid_history_coverage")
    for kind in SOURCE_KINDS[platform]:
        records = result.get(kind)
        if not isinstance(records, list) or any(not isinstance(record, dict) for record in records):
            raise ResultFailure("source_read_incomplete")
        if len(records) > MAX_RECORDS:
            raise ResultFailure("record_limit_exceeded")
    return result


def accept_source(result: dict[str, JSONValue], directory: Path) -> dict[str, int]:
    result["read_only_verified"] = True
    result["requested_credentials_verified"] = True
    result["read_only_evidence"] = "fresh_terminal_investor_authorization_and_connected_identity_and_trade_disabled"
    directory.mkdir(mode=0o700, exist_ok=True)
    temporary = directory / "source-read.partial"
    try:
        with temporary.open("w", encoding="utf-8") as output:
            os.fchmod(output.fileno(), 0o600)
            json.dump(result, output, ensure_ascii=False, allow_nan=False)
        temporary.replace(directory / "source-read.json")
    finally:
        temporary.unlink(missing_ok=True)
    platform = result["platform"]
    assert isinstance(platform, str)
    counts: dict[str, int] = {}
    for kind in SOURCE_KINDS[platform]:
        records = result[kind]
        assert isinstance(records, list)
        counts[kind] = len(records)
    return counts
