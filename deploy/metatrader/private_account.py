import argparse
import getpass
import json
import os
from pathlib import Path
import secrets
import sys
import time

from account_request import RequestFailure, validate_request


RUNTIME_UID = 10001
RUNTIME_GID = 10001


def write_private(path: Path, data: str) -> None:
    temporary = path.with_suffix(".partial")
    with temporary.open("w", encoding="utf-8") as output:
        os.fchmod(output.fileno(), 0o400)
        os.fchown(output.fileno(), RUNTIME_UID, RUNTIME_GID)
        output.write(data)
    temporary.replace(path)


def prepare(root: Path) -> None:
    directory = root / "secrets"
    directory.mkdir(mode=0o700, exist_ok=True)
    for platform in ("mt4", "mt5"):
        private = directory / platform
        private.mkdir(mode=0o500, exist_ok=True)
        os.chown(private, RUNTIME_UID, RUNTIME_GID)
        os.chmod(private, 0o500)
        if not (private / "desktop-password").exists():
            alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
            write_private(private / "desktop-password", "".join(secrets.choice(alphabet) for _ in range(8)) + "\n")


def configure(root: Path, platform: str) -> None:
    if not sys.stdin.isatty() or not sys.stdout.isatty():
        raise ValueError("Private account configuration requires an interactive terminal")
    login = getpass.getpass("Account login (private): ")
    server = getpass.getpass("Exact terminal broker server (private): ")
    password = getpass.getpass("Investor/read-only password (private): ")
    end = int(time.time())
    request = {"login": login, "server": server, "password": password,
               "from_epoch": str(end - 7 * 24 * 60 * 60), "to_epoch": str(end)}
    if platform == "mt4":
        symbol = getpass.getpass("Existing broker chart symbol (private): ")
        request["symbol"] = symbol
    request = validate_request(request, platform)
    prepare(root)
    write_private(root / "secrets" / platform / "account-request.json", json.dumps(request) + "\n")
    print(json.dumps({"event": "connector.private_account.configured", "platform": platform,
                      "connection_verified": False, "history_window_seconds": 7 * 24 * 60 * 60}))


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("command", choices=("prepare", "configure", "desktop-password", "refresh-window"))
    parser.add_argument("platform", choices=("mt4", "mt5"), nargs="?")
    parser.add_argument("--root", type=Path, default=Path("/opt/macro-terminal-connectivity"))
    arguments = parser.parse_args()
    if os.geteuid() != 0:
        raise ValueError("Private files must be managed by the host administrator")
    os.umask(0o077)
    if arguments.command == "prepare":
        prepare(arguments.root)
        print(json.dumps({"event": "connector.private_directories.prepared", "credentials_created": False}))
        return 0
    if arguments.platform is None:
        raise ValueError("Choose the terminal platform")
    if arguments.command == "configure":
        configure(arguments.root, arguments.platform)
    elif arguments.command == "refresh-window":
        path = arguments.root / "secrets" / arguments.platform / "account-request.json"
        request = json.loads(path.read_text())
        end = int(time.time())
        request["from_epoch"], request["to_epoch"] = str(end - 7 * 24 * 60 * 60), str(end)
        write_private(path, json.dumps(request) + "\n")
        print(json.dumps({"event": "connector.private_account.window_refreshed", "platform": arguments.platform}))
    else:
        if not sys.stdin.isatty() or not sys.stdout.isatty():
            raise ValueError("Desktop passwords may only be shown in a private interactive terminal")
        print((arguments.root / "secrets" / arguments.platform / "desktop-password").read_text().strip())
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, RequestFailure):
        print("Private configuration failed; no account details are printed", file=sys.stderr)
        sys.exit(1)
