import argparse
from contextlib import contextmanager
import fcntl
import hashlib
import json
import math
import os
from pathlib import Path, PureWindowsPath
import re
import selectors
import shlex
import shutil
import signal
import stat
import subprocess
import sys
import tempfile
import time
import urllib.request
import urllib.error
import zipfile
import uuid
from collections.abc import Iterator
from typing import TYPE_CHECKING

from account_request import AccountRequest, RequestFailure, load_private_request
from source_results import (
    ResultFailure, accept_source, investor_authorized, journal_cursor, read_source,
)

if TYPE_CHECKING:
    from telemetry import Telemetry


SOURCE = Path(__file__).resolve().parent
DATA = Path("/data")
PREFIX = DATA / "wine"
DIAGNOSTICS = DATA / "diagnostics"
HEALTH = Path("/tmp/metatrader-health.json")
MT4_REQUEST = Path("/tmp/metatrader-mt4-request.json")
MAX_COLLECTION_SECONDS = 120
MAX_DOWNLOAD_BYTES = 128 * 1024 * 1024
MAX_DIAGNOSTIC_BYTES = 1024 * 1024
MAX_EXTRACTED_BYTES = 256 * 1024 * 1024
MAX_SERVER_DEFINITION_BYTES = 4 * 1024 * 1024
TELEMETRY: "Telemetry | None" = None


class RuntimeFailure(Exception):
    def __init__(self, code: str):
        self.code = code
        super().__init__(code)


def event(name: str, platform: str, **attributes: str | int | bool) -> None:
    from telemetry import safe_attributes
    attributes = safe_attributes(attributes)
    correlation = TELEMETRY.event(name, attributes) if TELEMETRY is not None else {}
    print(json.dumps({
        "event": name,
        "service.name": "macro-terminal-connectivity",
        "platform": platform,
        **correlation,
        **attributes,
    }), flush=True)


def download(artifact_cache: Path | None = None) -> None:
    artifacts = json.loads((SOURCE / "artifacts.json").read_text())
    directory = SOURCE / "artifacts"
    directory.mkdir(exist_ok=True)
    for name, artifact in artifacts.items():
        digest = hashlib.sha256()
        size = 0
        filename = artifact["filename"]
        if Path(filename).name != filename or not artifact["url"].startswith("https://"):
            raise RuntimeFailure("invalid_artifact")
        target = directory / filename
        temporary = target.with_suffix(target.suffix + ".partial")
        cached = artifact_cache / filename if artifact_cache is not None else None
        acquisition = "https"
        try:
            if cached is not None and cached.is_file():
                if "sha256" not in artifact or cached.is_symlink():
                    raise RuntimeFailure("unverified_artifact_cache")
                source = cached.open("rb")
                acquisition = "verified_cache"
            else:
                source = urllib.request.urlopen(urllib.request.Request(
                    artifact["url"], headers={"User-Agent": "MacroTerminalConnectivity/0.1"},
                ), timeout=15)
                if not source.url.startswith("https://"):
                    source.close()
                    raise RuntimeFailure("insecure_download_redirect")
            deadline = time.monotonic() + 180
            with source, temporary.open("wb") as output:
                while chunk := source.read(64 * 1024):
                    size += len(chunk)
                    if size > MAX_DOWNLOAD_BYTES:
                        raise RuntimeFailure("artifact_too_large")
                    if time.monotonic() > deadline:
                        raise RuntimeFailure("artifact_download_timeout")
                    digest.update(chunk)
                    output.write(chunk)
            checksum = digest.hexdigest()
            if "sha256" in artifact and checksum != artifact["sha256"]:
                raise RuntimeFailure("artifact_checksum_mismatch")
            if filename.endswith(".exe"):
                with temporary.open("rb") as executable:
                    if executable.read(2) != b"MZ":
                        raise RuntimeFailure("invalid_installer_payload")
            temporary.replace(target)
        except urllib.error.HTTPError as error:
            event("connector.artifact.download_failed", "runtime", artifact=name, http_status=error.code)
            raise RuntimeFailure("artifact_download_refused") from error
        finally:
            temporary.unlink(missing_ok=True)
        artifact["downloaded_sha256"] = checksum
        artifact["bytes"] = size
        artifact["acquisition"] = acquisition
        event("connector.artifact.downloaded", "runtime", artifact=name, bytes=size, acquisition=acquisition)
    (directory / "manifest.json").write_text(json.dumps(artifacts, indent=2) + "\n")


def stop_process(process: subprocess.Popen[bytes]) -> None:
    try:
        os.killpg(process.pid, signal.SIGTERM)
    except ProcessLookupError:
        pass
    try:
        process.wait(timeout=3)
    except subprocess.TimeoutExpired:
        pass
    try:
        os.killpg(process.pid, signal.SIGKILL)
    except ProcessLookupError:
        pass
    process.wait(timeout=3)


@contextmanager
def operation(name: str) -> Iterator[None]:
    if TELEMETRY is None:
        yield
    else:
        with TELEMETRY.operation(name):
            yield


def run(command: list[str], name: str, timeout: int, *, check: bool = True,
        env: dict[str, str] | None = None) -> int:
    with operation(name):
        return run_process(command, name, timeout, check=check, env=env)


def run_process(command: list[str], name: str, timeout: int, *, check: bool = True,
                env: dict[str, str] | None = None) -> int:
    with (DIAGNOSTICS / f"{name}.log").open("wb") as output:
        process = subprocess.Popen(
            command, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT, start_new_session=True, env=env,
        )
        assert process.stdout is not None
        deadline = time.monotonic() + timeout
        written = 0
        try:
            with selectors.DefaultSelector() as selector:
                selector.register(process.stdout, selectors.EVENT_READ)
                while selector.get_map():
                    remaining = deadline - time.monotonic()
                    if remaining <= 0:
                        raise RuntimeFailure(f"{name}_timeout")
                    for key, _ in selector.select(min(remaining, 0.5)):
                        chunk = os.read(key.fd, 64 * 1024)
                        if not chunk:
                            selector.unregister(key.fileobj)
                            continue
                        retained = chunk[:max(0, MAX_DIAGNOSTIC_BYTES - written)]
                        output.write(retained)
                        written += len(retained)
                    if process.poll() is not None:
                        break
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    raise RuntimeFailure(f"{name}_timeout")
                try:
                    returncode = process.wait(timeout=remaining)
                except subprocess.TimeoutExpired as error:
                    raise RuntimeFailure(f"{name}_timeout") from error
        finally:
            stop_process(process)
            process.stdout.close()
    if check and returncode != 0:
        raise RuntimeFailure(f"{name}_failed")
    return returncode


def display_ready() -> bool:
    try:
        result = subprocess.run(
            ["xdpyinfo"], stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL, timeout=2, check=False,
        )
        return result.returncode == 0
    except subprocess.TimeoutExpired:
        return False


def start_display(processes: list[subprocess.Popen[bytes]]) -> None:
    xvfb = subprocess.Popen(
        ["Xvfb", ":99", "-screen", "0", "1280x800x24", "-nolisten", "tcp", "-noreset"],
        stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
        start_new_session=True,
    )
    processes.append(xvfb)
    deadline = time.monotonic() + 15
    while time.monotonic() < deadline:
        if xvfb.poll() is not None:
            raise RuntimeFailure("display_start_failed")
        if display_ready():
            window_manager = subprocess.Popen(
                ["openbox", "--sm-disable"], stdin=subprocess.DEVNULL,
                stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                start_new_session=True,
            )
            processes.append(window_manager)
            return
        time.sleep(0.2)
    raise RuntimeFailure("display_start_timeout")


def terminal_path(platform: str) -> Path:
    return PREFIX / "drive_c" / "Terminal" / (
        "terminal64.exe" if platform == "mt5" else "terminal.exe"
    )


def extract(archive_path: Path, destination: Path) -> None:
    with zipfile.ZipFile(archive_path) as archive:
        if sum(member.file_size for member in archive.infolist()) > MAX_EXTRACTED_BYTES:
            raise RuntimeFailure("archive_too_large")
        for member in archive.infolist():
            path = Path(member.filename.replace("\\", "/"))
            if path.is_absolute() or ".." in path.parts or ":" in member.filename:
                raise RuntimeFailure("unsafe_archive_path")
            if member.external_attr >> 16 & 0o170000 == 0o120000:
                raise RuntimeFailure("unsafe_archive_symlink")
        archive.extractall(destination)


def configuration(platform: str) -> str:
    if platform == "mt4":
        return (
            "EnableNews=false\nEnableDDE=false\nFTPEnable=false\n"
            "ExpertsEnable=false\nExpertsTrades=false\n"
            "ExpertsDllImport=false\nExpertsExpImport=false\n"
        )
    return (
        "[Common]\nKeepPrivate=0\nNewsEnable=0\n"
        "[Experts]\nEnabled=0\nAllowLiveTrading=0\nAllowDllImport=0\n"
    )


def compile_mt4() -> None:
    scripts = PREFIX / "drive_c/Terminal/MQL4/Scripts"
    scripts.mkdir(parents=True, exist_ok=True)
    for name in ("ReadOnlyProbe", "ReadOnlyCollector"):
        source = scripts / f"{name}.mq4"
        payload = source.with_suffix(".ex4")
        log = source.with_suffix(".log")
        shutil.copyfile(SOURCE / f"mt4/{name}.mq4", source)
        payload.unlink(missing_ok=True)
        log.unlink(missing_ok=True)
        run([
            "wine", str(PREFIX / "drive_c/Terminal/metaeditor.exe"),
            "/portable", f"/compile:C:\\Terminal\\MQL4\\Scripts\\{name}.mq4", "/log",
        ], "mt4_compile", 90, check=False)
        raw = log.read_bytes() if log.is_file() else b""
        text = raw.decode("utf-16") if raw.startswith((b"\xff\xfe", b"\xfe\xff")) else raw.decode("utf-8")
        if not re.search(r"\b0 errors,\s*0 warnings\b", text):
            raise RuntimeFailure("mt4_compile_diagnostics")
        if not payload.is_file() or payload.stat().st_size == 0:
            raise RuntimeFailure("mt4_compiled_payload_missing")
        event("connector.mt4.compiled", "mt4", collector=name)


def install_tools(platform: str) -> None:
    if platform == "mt4":
        compile_mt4()
        return
    python = PREFIX / "drive_c/Python"
    python.mkdir(exist_ok=True)
    extract(SOURCE / "artifacts/python.zip", python)
    packages = python / "Lib/site-packages"
    packages.mkdir(parents=True, exist_ok=True)
    for name in ["numpy", "metatrader5"]:
        extract(SOURCE / f"artifacts/{name}.whl", packages)
    (python / "python314._pth").write_text(
        "python314.zip\n.\nLib\\site-packages\nC:\\MacroTerminal\nimport site\n"
    )
    scripts = PREFIX / "drive_c/MacroTerminal"
    scripts.mkdir(exist_ok=True)
    for name in ("mt5_probe.py", "mt5_collect.py", "account_request.py"):
        shutil.copyfile(SOURCE / name, scripts / name)
    returncode = run([
        "script", "--quiet", "--return", "--command", shlex.join([
            "wine", "C:\\Python\\python.exe", "C:\\MacroTerminal\\mt5_probe.py",
        ]), "/dev/null",
    ], "mt5_package_check", 90, check=False)
    publish_probe_events(DIAGNOSTICS / "mt5_package_check.log")
    if returncode != 0:
        raise RuntimeFailure("windows_package_import_failed")


def initialize_wine() -> None:
    bootstrap = {**os.environ, "WINEARCH": "wow64", "WINEBOOTSTRAPMODE": "1"}
    run(["wineboot", "--update" if PREFIX.exists() else "--init"], "wineboot", 300, env=bootstrap)
    subprocess.run(["wineserver", "--kill"], timeout=10, check=False,
                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    run(["wineserver", "--wait"], "wineboot_cleanup", 20)
    normal = {**os.environ, "WINEARCH": "win64"}
    normal.pop("WINEBOOTSTRAPMODE", None)
    for architecture, directory in (("64", "system32"), ("32", "syswow64")):
        run(["wine", f"C:\\windows\\{directory}\\cmd.exe", "/c", "exit", "0"],
            f"wine{architecture}_check", 45, env=normal)


def webview2_version() -> str | None:
    application = "{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}"
    normal = {**os.environ, "WINEARCH": "win64"}
    normal.pop("WINEBOOTSTRAPMODE", None)
    locations = (
        ("machine", "HKLM\\Software\\Wow6432Node\\Microsoft\\EdgeUpdate"),
        ("user", "HKCU\\Software\\Microsoft\\EdgeUpdate"),
    )
    for scope, key in locations:
        values: dict[str, str] = {}
        for value, branch, suffix in (("pv", "Clients", "version"), ("EBWebView", "ClientState", "location")):
            name = f"webview2_{scope}_{suffix}"
            if run(["wine", "C:\\windows\\system32\\reg.exe", "query",
                    f"{key}\\{branch}\\{application}", "/v", value], name, 30,
                   check=False, env=normal) != 0:
                break
            text = (DIAGNOSTICS / f"{name}.log").read_text(errors="replace")
            match = re.search(rf"^\s*{value}\s+REG_SZ\s+([^\r\n]+)\s*$", text, re.MULTILINE)
            if match is None:
                break
            values[value] = match[1].strip()
        version = values.get("pv", "")
        if not re.fullmatch(r"[0-9]{1,6}(?:\.[0-9]{1,6}){3}", version) or not any(int(part) for part in version.split(".")):
            continue
        location = PureWindowsPath(values.get("EBWebView", ""))
        if (location.drive.lower() != "c:" or location.root != "\\" or ".." in location.parts
                or tuple(part.lower() for part in location.parts[-4:-1]) != ("microsoft", "edgewebview", "application")
                or location.name != version):
            continue
        directory = PREFIX / "drive_c" / Path(*location.parts[1:])
        if not directory.resolve().is_relative_to((PREFIX / "drive_c").resolve()):
            continue
        if all(windows_payload(directory / name) for name in ("msedgewebview2.exe", "msedge.dll")):
            return version
    return None


def windows_payload(path: Path) -> bool:
    if not path.is_file() or path.is_symlink():
        return False
    with path.open("rb") as source:
        return stat.S_ISREG(os.fstat(source.fileno()).st_mode) and source.read(2) == b"MZ"


def ensure_webview2() -> None:
    if version := webview2_version():
        event("connector.webview2.detected", "mt5", webview2_version=version, browser_execution_verified=False)
        return
    installer_exit: int | None = None
    timed_out = False
    try:
        installer_exit = run(["wine", str(SOURCE / "artifacts/webview2setup.exe"), "/silent", "/install"],
                             "webview2_install", 300, check=False)
    except RuntimeFailure as error:
        if error.code != "webview2_install_timeout":
            raise
        timed_out = True
    finally:
        subprocess.run(["wineserver", "--kill"], timeout=10, check=False,
                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        run(["wineserver", "--wait"], "webview2_cleanup", 20)
    version = webview2_version()
    if version is None:
        raise RuntimeFailure("webview2_install_timeout" if timed_out else "webview2_registration_not_verified")
    attributes: dict[str, str | int | bool] = {
        "webview2_version": version, "installer_timed_out": timed_out, "browser_execution_verified": False,
    }
    if installer_exit is not None:
        attributes["installer_exit"] = installer_exit
    event("connector.webview2.registered", "mt5", **attributes)


def terminal_payloads(platform: str) -> bool:
    terminal = terminal_path(platform)
    name = "metaeditor64.exe" if platform == "mt5" else "metaeditor.exe"
    editors = [path for path in terminal.parent.glob("*") if path.name.lower() == name]
    return windows_payload(terminal) and len(editors) == 1 and windows_payload(editors[0])


def setup(platform: str) -> None:
    if (DATA / "provisioned.json").exists():
        raise RuntimeFailure("runtime_already_provisioned")
    initialize_wine()
    run(["winecfg", "-v=win11" if platform == "mt5" else "-v=win10"], "winecfg", 30)
    if platform == "mt5":
        ensure_webview2()
    installer_exit: int | None = None
    if not terminal_payloads(platform):
        installer_exit = run([
            "wine", str(SOURCE / f"artifacts/{platform}setup.exe"),
            "/auto", "/path:C:\\Terminal",
        ], "terminal_install", 300, check=False)
        if not terminal_payloads(platform):
            raise RuntimeFailure("terminal_payload_missing")

    subprocess.run(["wineserver", "--kill"], timeout=10, check=False,
                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    run(["wineserver", "--wait"], "installer_cleanup", 20)
    install_tools(platform)
    attributes: dict[str, int | bool] = {"collector_install_verified": True}
    if installer_exit is not None:
        attributes["installer_exit"] = installer_exit
    event("connector.terminal.installed", platform, **attributes)

    (PREFIX / "drive_c/Terminal/runtime.ini").write_text(configuration(platform))
    shutil.copyfile(SOURCE / "artifacts/manifest.json", DATA / "artifacts.json")
    (DATA / "provisioned.json").write_text(json.dumps({
        "platform": platform, "account_configured": False,
    }) + "\n")
    event("connector.runtime.provisioned", platform, account_configured=False)


@contextmanager
def operation_lock() -> Iterator[None]:
    with (DATA / "operation.lock").open("a") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as error:
            raise RuntimeFailure("terminal_operation_busy") from error
        yield


def publish_probe_events(path: Path) -> None:
    allowed = {"python", "python_bits", "numpy", "metatrader5", "initialized", "error_code",
               "account_configured", "broker_connectivity_verified", "terminal_version", "terminal_build",
               "outcome", "orders", "positions", "history_orders", "history_deals"}
    text = re.sub(r"\x1b\[[0-?]*[ -/]*[@-~]", "", path.read_text(errors="replace"))
    decoder = json.JSONDecoder()
    for match in re.finditer(r'\{"event":\s*"connector\.mt5\.', text):
        try:
            record, _ = decoder.raw_decode(text, match.start())
        except ValueError:
            continue
        if (record.get("event") in {"connector.mt5.package_import_failed", "connector.mt5.package_imported",
                                    "connector.mt5.ipc_probe", "connector.mt5.ipc_probe_failed",
                                    "connector.mt5.terminal_version", "connector.mt5.source_read"}
                and all(isinstance(value, (str, int, bool)) for value in record.values())):
            event(record["event"], "mt5", **{key: value for key, value in record.items() if key in allowed})


def probe(ipc: bool) -> None:
    if not (DATA / "provisioned.json").is_file():
        raise RuntimeFailure("runtime_not_provisioned")
    command = ["wine", "C:\\Python\\python.exe", "C:\\MacroTerminal\\mt5_probe.py"]
    if ipc:
        command.append("--ipc")
    if health() != 0:
        raise RuntimeFailure("runtime_not_running")
    with operation_lock():
        returncode = run(
            ["script", "--quiet", "--return", "--command", shlex.join(command), "/dev/null"],
            "mt5_probe", 90, check=False,
        )
    publish_probe_events(DIAGNOSTICS / "mt5_probe.log")
    if returncode != 0:
        raise RuntimeFailure("mt5_probe_failed")


def desktop() -> None:
    password = Path("/run/secrets/desktop-password")
    if health() != 0:
        raise RuntimeFailure("runtime_not_running")
    if not password.is_file() or stat.S_IMODE(password.stat().st_mode) & 0o077:
        raise RuntimeFailure("private_desktop_password_required")
    run([
        "x11vnc", "-display", ":99", "-passwdfile", str(password),
        "-rfbport", "5900", "-forever", "-shared", "-noxdamage", "-quiet",
    ], "desktop", 1800)


def collect_mt5() -> None:
    if health() != 0:
        raise RuntimeFailure("runtime_not_running")
    request_path = Path("/run/secrets/account-request.json")
    request = load_private_request(request_path, "mt5")
    output = PREFIX / "drive_c/MacroTerminal/source-read.json"
    with operation_lock(), operation("source_read"):
        output.unlink(missing_ok=True)
        try:
            attempt = uuid.uuid4().hex
            journal = PREFIX / "drive_c/Terminal/logs"
            cursor = journal_cursor(journal)
            returncode = run([
                "script", "--quiet", "--return", "--command", shlex.join([
                    "wine", "C:\\Python\\python.exe", "C:\\MacroTerminal\\mt5_collect.py",
                    "--request", "Z:\\run\\secrets\\account-request.json",
                    "--output", "C:\\MacroTerminal\\source-read.json", "--attempt", attempt,
                ]), "/dev/null",
            ], "mt5_collect", MAX_COLLECTION_SECONDS, check=False)
            publish_probe_events(DIAGNOSTICS / "mt5_collect.log")
            if returncode != 0 or not output.is_file():
                raise RuntimeFailure("source_read_failed")
            result = read_source(output, "mt5", request, attempt)
            if not investor_authorized(journal, request["login"], cursor):
                raise RuntimeFailure("fresh_investor_authorization_not_verified")
            counts = accept_source(result, DATA / "results")
            if TELEMETRY is not None:
                TELEMETRY.accepted_records(counts)
            event("connector.source.accepted", "mt5", read_only_verified=True, **counts)
        finally:
            output.unlink(missing_ok=True)
            output.with_suffix(".partial").unlink(missing_ok=True)


def collect_mt4() -> None:
    if health() != 0:
        raise RuntimeFailure("runtime_not_running")
    load_private_request(Path("/run/secrets/account-request.json"), "mt4")
    with operation_lock():
        if MT4_REQUEST.exists():
            raise RuntimeFailure("terminal_operation_busy")
        attempt = uuid.uuid4().hex
        response = MT4_REQUEST.with_name(f"metatrader-mt4-response-{attempt}.json")
        temporary = MT4_REQUEST.with_suffix(".partial")
        deadline = time.monotonic() + MAX_COLLECTION_SECONDS
        try:
            temporary.write_text(json.dumps({"attempt_id": attempt, "deadline": deadline, "client_pid": os.getpid()}))
            temporary.replace(MT4_REQUEST)
            while time.monotonic() < deadline + 15:
                if response.is_file():
                    result = json.loads(response.read_text())
                    if result.get("attempt_id") != attempt:
                        raise RuntimeFailure("source_attempt_mismatch")
                    if result.get("outcome") != "accepted":
                        code = result.get("error_code", "source_read_failed")
                        if not isinstance(code, str) or not re.fullmatch(r"[a-z][a-z0-9_]{0,79}", code):
                            code = "source_read_failed"
                        raise RuntimeFailure(code)
                    event("connector.source.request_completed", "mt4", read_only_verified=True)
                    return
                if health() != 0:
                    raise RuntimeFailure("runtime_not_running")
                time.sleep(0.2)
            raise RuntimeFailure("source_read_timeout")
        finally:
            MT4_REQUEST.unlink(missing_ok=True)
            temporary.unlink(missing_ok=True)
            response.unlink(missing_ok=True)


def start_terminal(platform: str, processes: list[subprocess.Popen[bytes]], executable: Path | None = None,
                   config: str = "C:\\Terminal\\runtime.ini") -> None:
    process = subprocess.Popen([
        "wine", str(executable or terminal_path(platform)), "/portable",
        config if platform == "mt4" else f"/config:{config}",
    ], stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
        start_new_session=True)
    processes.append(process)


def refresh_health(processes: list[subprocess.Popen[bytes]]) -> None:
    if any(child.poll() is not None for child in processes):
        raise RuntimeFailure("runtime_process_exited")
    temporary = HEALTH.with_suffix(".partial")
    temporary.write_text(json.dumps({"observed_at": time.monotonic(), "pid": os.getpid()}))
    temporary.replace(HEALTH)
    if TELEMETRY is not None:
        TELEMETRY.runtime_healthy = 1


def stop_terminal(processes: list[subprocess.Popen[bytes]]) -> None:
    stop_process(processes[-1])
    subprocess.run(["wineserver", "--kill"], timeout=10, check=False,
                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    run(["wineserver", "--wait"], "terminal_cleanup", 10)
    processes.pop()


def copy_mt4_servers(destination: Path, server: str) -> None:
    config = terminal_path("mt4").parent / "config"
    copied = False
    for name in ("servers.ini", server + ".srv"):
        source = config / name
        if not source.exists() and not source.is_symlink():
            continue
        try:
            descriptor = os.open(source, os.O_RDONLY | os.O_NOFOLLOW)
        except OSError as error:
            raise RuntimeFailure("invalid_broker_server_definition") from error
        with os.fdopen(descriptor, "rb") as handle:
            if not stat.S_ISREG(os.fstat(handle.fileno()).st_mode):
                raise RuntimeFailure("invalid_broker_server_definition")
            payload = handle.read(MAX_SERVER_DEFINITION_BYTES + 1)
        if not payload or len(payload) > MAX_SERVER_DEFINITION_BYTES:
            raise RuntimeFailure("invalid_broker_server_definition")
        if name == "servers.ini" and ("Copyright ".encode("utf-16-le") not in payload[:256]
                                      or "Servers\0".encode("utf-16-le") not in payload[:256]):
            raise RuntimeFailure("invalid_broker_server_catalog")
        (destination / name).write_bytes(payload)
        copied = True
    if not copied:
        raise RuntimeFailure("broker_server_definition_required")


def prepare_mt4_request(directory: Path, request: AccountRequest, attempt: str) -> Path:
    scripts = directory / "MQL4/Scripts"
    presets = directory / "MQL4/Presets"
    config = directory / "config"
    for path in (scripts, presets, config):
        path.mkdir(mode=0o700, parents=True)
    terminal = terminal_path("mt4")
    for source in (terminal, *terminal.parent.glob("*.dll")):
        if not windows_payload(source):
            raise RuntimeFailure("terminal_payload_missing")
        shutil.copyfile(source, directory / source.name)
    copy_mt4_servers(config, request["server"])
    collector = terminal.parent / "MQL4/Scripts/ReadOnlyCollector.ex4"
    if not collector.is_file() or collector.is_symlink() or collector.stat().st_size == 0:
        raise RuntimeFailure("mt4_compiled_payload_missing")
    shutil.copyfile(collector, scripts / "ReadOnlyCollector.ex4")
    (presets / "collector.set").write_text(
        f"ExpectedLogin={request['login']}\nExpectedServer={request['server']}\n"
        f"HistoryFrom={request['from_epoch']}\nHistoryTo={request['to_epoch']}\nAttemptId={attempt}\n",
        encoding="ascii",
    )
    startup = directory / "request.ini"
    with startup.open("w", encoding="ascii") as output:
        os.fchmod(output.fileno(), 0o600)
        output.write(
            configuration("mt4") + f"Login={request['login']}\nPassword={request['password']}\n"
            f"Server={request['server']}\nSymbol={request['symbol']}\nPeriod=M1\n"
            "Script=ReadOnlyCollector\nScriptParameters=collector.set\n"
        )
    return startup


def check_mt4_client(attempt: str, client_pid: int) -> None:
    try:
        os.kill(client_pid, 0)
        current = json.loads(MT4_REQUEST.read_text())
    except (OSError, ValueError) as error:
        raise RuntimeFailure("source_request_cancelled") from error
    if not isinstance(current, dict) or current.get("attempt_id") != attempt:
        raise RuntimeFailure("source_request_cancelled")


def supervise_mt4_request(processes: list[subprocess.Popen[bytes]]) -> None:
    queue = json.loads(MT4_REQUEST.read_text())
    attempt = queue.get("attempt_id")
    deadline = queue.get("deadline")
    client_pid = queue.get("client_pid")
    if (not isinstance(attempt, str) or not re.fullmatch(r"[a-f0-9]{32}", attempt)
            or not isinstance(deadline, (int, float)) or not math.isfinite(deadline)
            or not isinstance(client_pid, int) or client_pid <= 0):
        raise RuntimeFailure("invalid_source_request")
    response = MT4_REQUEST.with_name(f"metatrader-mt4-response-{attempt}.json")
    if response.exists():
        return
    result: dict[str, str | bool] = {"attempt_id": attempt, "outcome": "failed"}
    try:
        with operation("source_read"):
            if not 0 < deadline - time.monotonic() <= MAX_COLLECTION_SECONDS:
                raise RuntimeFailure("source_read_timeout")
            check_mt4_client(attempt, client_pid)
            request = load_private_request(Path("/run/secrets/account-request.json"), "mt4")
            private = tempfile.TemporaryDirectory(prefix="macro-terminal-mt4-", dir="/tmp", delete=False)
            directory = Path(private.name)
            idle_stopped = False
            request_started = False
            try:
                stop_terminal(processes)
                idle_stopped = True
                startup = prepare_mt4_request(directory, request, attempt)
                check_mt4_client(attempt, client_pid)
                start_terminal("mt4", processes, directory / "terminal.exe", "Z:" + str(startup).replace("/", "\\"))
                request_started = True
                output = directory / "MQL4/Files/macro-terminal-source.json"
                while time.monotonic() < deadline:
                    refresh_health(processes)
                    check_mt4_client(attempt, client_pid)
                    if output.is_file():
                        snapshot = read_source(output, "mt4", request, attempt)
                        if investor_authorized(directory / "logs", request["login"], {}):
                            counts = accept_source(snapshot, DATA / "results")
                            if TELEMETRY is not None:
                                TELEMETRY.accepted_records(counts)
                            event("connector.source.accepted", "mt4", read_only_verified=True, **counts)
                            result["outcome"] = "accepted"
                            return
                        if deadline - time.monotonic() <= 2:
                            raise RuntimeFailure("fresh_investor_authorization_not_verified")
                    time.sleep(0.2)
                raise RuntimeFailure("source_read_timeout")
            finally:
                if request_started:
                    stop_terminal(processes)
                private.cleanup()
                if idle_stopped:
                    start_terminal("mt4", processes)
                    refresh_health(processes)
    except (RuntimeFailure, RequestFailure, ResultFailure) as error:
        result["outcome"] = "failed"
        result["error_code"] = error.code
        if isinstance(error, RuntimeFailure) and error.code.startswith("terminal_cleanup_"):
            raise
    except (OSError, ValueError, TypeError):
        result["error_code"] = "source_read_failed"
    finally:
        temporary = response.with_suffix(".partial")
        temporary.write_text(json.dumps(result))
        temporary.replace(response)
        if not MT4_REQUEST.exists():
            response.unlink(missing_ok=True)


def serve(platform: str, processes: list[subprocess.Popen[bytes]]) -> None:
    if not (DATA / "provisioned.json").is_file() or not terminal_path(platform).is_file():
        raise RuntimeFailure("runtime_not_provisioned")
    state = json.loads((DATA / "provisioned.json").read_text())
    if state.get("platform") != platform:
        raise RuntimeFailure("runtime_platform_mismatch")
    MT4_REQUEST.unlink(missing_ok=True)
    for path in MT4_REQUEST.parent.glob("metatrader-mt4-response-*.json"):
        path.unlink(missing_ok=True)
    start_terminal(platform, processes)
    event("connector.terminal.started", platform, account_configured=False)
    while True:
        refresh_health(processes)
        if platform == "mt4" and MT4_REQUEST.is_file():
            supervise_mt4_request(processes)
        time.sleep(0.5)


def health() -> int:
    try:
        status = json.loads(HEALTH.read_text())
        os.kill(status["pid"], 0)
        age = time.monotonic() - status["observed_at"]
        return 0 if 0 <= age < 10 and display_ready() else 1
    except (OSError, ValueError, TypeError, KeyError):
        return 1


def main() -> int:
    global TELEMETRY
    parser = argparse.ArgumentParser()
    parser.add_argument("command", choices=["download", "setup", "serve", "probe", "health", "tools", "desktop", "collect"])
    parser.add_argument("--ipc", action="store_true")
    parser.add_argument("--artifact-cache", type=Path)
    arguments = parser.parse_args()
    os.umask(0o022 if arguments.command == "download" else 0o077)
    if arguments.command == "download":
        download(arguments.artifact_cache)
        return 0
    if arguments.command == "health":
        return health()

    def stop(_signum: int, _frame: object) -> None:
        raise KeyboardInterrupt

    signal.signal(signal.SIGTERM, stop)
    signal.signal(signal.SIGINT, stop)
    platform = os.environ.get("MACRO_TERMINAL_PLATFORM", "")
    if platform not in ("mt4", "mt5"):
        raise RuntimeFailure("invalid_platform")
    if endpoint := os.environ.get("OTEL_EXPORTER_OTLP_ENDPOINT"):
        from telemetry import Telemetry
        TELEMETRY = Telemetry(platform, arguments.command, endpoint)
    if arguments.command == "desktop":
        desktop()
        return 0
    if arguments.command == "collect":
        if platform == "mt5":
            collect_mt5()
        else:
            collect_mt4()
        return 0
    if arguments.command == "probe":
        if platform != "mt5":
            raise RuntimeFailure("probe_requires_mt5")
        probe(arguments.ipc)
        return 0

    (DATA / "home").mkdir(exist_ok=True)
    Path(os.environ.get("XDG_RUNTIME_DIR", "/tmp/runtime-terminal")).mkdir(mode=0o700, exist_ok=True)
    DIAGNOSTICS.mkdir(mode=0o700, exist_ok=True)
    processes: list[subprocess.Popen[bytes]] = []

    with (DATA / "runtime.lock").open("a") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as error:
            raise RuntimeFailure("runtime_already_owned") from error
        try:
            HEALTH.unlink(missing_ok=True)
            start_display(processes)
            if arguments.command == "setup":
                setup(platform)
            elif arguments.command == "tools":
                if not terminal_path(platform).is_file():
                    raise RuntimeFailure("terminal_payload_missing")
                install_tools(platform)
            else:
                serve(platform, processes)
        except KeyboardInterrupt:
            event("connector.runtime.stopped", platform)
        finally:
            try:
                subprocess.run(["wineserver", "--kill"], timeout=10, check=False,
                               stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            finally:
                for process in reversed(processes):
                    stop_process(process)
                HEALTH.unlink(missing_ok=True)
                if TELEMETRY is not None:
                    TELEMETRY.runtime_healthy = 0
    return 0


if __name__ == "__main__":
    exitcode = 0
    try:
        exitcode = main()
    except (RuntimeFailure, RequestFailure, ResultFailure) as error:
        event("connector.runtime.failed", os.environ.get("MACRO_TERMINAL_PLATFORM", "runtime"),
              error_code=error.code)
        exitcode = 1
    except KeyboardInterrupt:
        event("connector.runtime.cancelled", os.environ.get("MACRO_TERMINAL_PLATFORM", "runtime"))
        exitcode = 130
    except Exception:
        event("connector.runtime.failed", os.environ.get("MACRO_TERMINAL_PLATFORM", "runtime"),
              error_code="runtime_operation_failed")
        exitcode = 1
    finally:
        if TELEMETRY is not None:
            TELEMETRY.close()
    sys.exit(exitcode)
