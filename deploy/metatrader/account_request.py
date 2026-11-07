import json
import os
from pathlib import Path
import stat
import time
from typing import NotRequired, TypedDict


MAX_REQUEST_BYTES = 16 * 1024
MAX_WINDOW_SECONDS = 7 * 24 * 60 * 60


class AccountRequest(TypedDict):
    login: str
    server: str
    password: str
    from_epoch: str
    to_epoch: str
    symbol: NotRequired[str]


class RequestFailure(Exception):
    def __init__(self, code: str):
        self.code = code
        super().__init__(code)


def validate_request(value: object, platform: str = "mt5") -> AccountRequest:
    required = {"login", "server", "password", "from_epoch", "to_epoch"}
    if platform == "mt4":
        required.add("symbol")
    if platform not in ("mt4", "mt5") or not isinstance(value, dict) or set(value) != required:
        raise RequestFailure("invalid_request")
    if any(not isinstance(item, str) for item in value.values()):
        raise RequestFailure("invalid_request")
    for key in ("login", "from_epoch", "to_epoch"):
        number = value[key]
        if not number.isascii() or not number.isdecimal() or len(number) > 20:
            raise RequestFailure("invalid_request")
    login, start, end = (int(value[key]) for key in ("login", "from_epoch", "to_epoch"))
    if not 0 < login <= 2**63 - 1:
        raise RequestFailure("invalid_request")
    if start <= 0 or end <= start or end - start > MAX_WINDOW_SECONDS or end > int(time.time()) + 60:
        raise RequestFailure("invalid_history_window")
    for key in required - {"login", "from_epoch", "to_epoch"}:
        text = value[key]
        if not text or len(text) > (128 if key == "symbol" else 256):
            raise RequestFailure("invalid_request")
        if any(ord(character) < 32 or ord(character) == 127 for character in text):
            raise RequestFailure("invalid_request")
    request: AccountRequest = {
        "login": str(login), "server": value["server"], "password": value["password"],
        "from_epoch": str(start), "to_epoch": str(end),
    }
    if platform == "mt4":
        if any(not value[key].isascii() for key in ("server", "password", "symbol")):
            raise RequestFailure("invalid_request")
        if any(character in value["server"] for character in '\\/:*?"<>|') or value["server"].endswith((".", " ")):
            raise RequestFailure("invalid_request")
        request["symbol"] = value["symbol"]
    return request


def load_private_request(path: Path, platform: str) -> AccountRequest:
    try:
        descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    except OSError as error:
        raise RequestFailure("private_account_request_required") from error
    with os.fdopen(descriptor, "rb") as source:
        metadata = os.fstat(source.fileno())
        if not stat.S_ISREG(metadata.st_mode) or stat.S_IMODE(metadata.st_mode) & 0o077:
            raise RequestFailure("private_account_request_required")
        raw = source.read(MAX_REQUEST_BYTES + 1)
    if len(raw) > MAX_REQUEST_BYTES:
        raise RequestFailure("invalid_request")
    try:
        value = json.loads(raw)
    except (ValueError, UnicodeError) as error:
        raise RequestFailure("invalid_request") from error
    return validate_request(value, platform)
