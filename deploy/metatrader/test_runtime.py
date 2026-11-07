import importlib.util
import hashlib
import json
import os
from pathlib import Path
import signal
import shutil
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import patch
import zipfile

from account_request import AccountRequest, RequestFailure, load_private_request
from source_results import ResultFailure, accept_source, investor_authorized, journal_cursor, read_source


SPEC = importlib.util.spec_from_file_location("metatrader_runtime", Path(__file__).with_name("runtime.py"))
assert SPEC is not None and SPEC.loader is not None
runtime = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(runtime)


class RuntimeTests(unittest.TestCase):
    def mt4_fixture(self, root: Path) -> AccountRequest:
        terminal = root / "wine/drive_c/Terminal"
        (terminal / "MQL4/Scripts").mkdir(parents=True)
        (terminal / "config").mkdir()
        (terminal / "terminal.exe").write_bytes(b"MZterminal")
        (terminal / "MQL4/Scripts/ReadOnlyCollector.ex4").write_bytes(b"compiled-collector")
        (terminal / "config/servers.ini").write_bytes(
            b"\x91\x01\0\0" + "Copyright 2000-2026, MetaQuotes Ltd.\0Servers\0".encode("utf-16-le") + b"opaque-catalog"
        )
        end = int(time.time())
        return {"login": "123", "server": "fixture-server", "password": "fixture-password",
                "from_epoch": str(end - 3600), "to_epoch": str(end), "symbol": "EURUSD"}

    def test_finished_command_does_not_wait_for_detached_output_holders(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            command = [sys.executable, "-c", (
                "import pathlib,subprocess,sys; "
                "child=subprocess.Popen([sys.executable,'-c','import time;time.sleep(30)'],start_new_session=True); "
                f"pathlib.Path({str(root / 'child')!r}).write_text(str(child.pid))"
            )]
            try:
                with patch.object(runtime, "DIAGNOSTICS", root):
                    self.assertEqual(runtime.run(command, "detached_output", 2), 0)
            finally:
                if (root / "child").exists():
                    os.kill(int((root / "child").read_text()), signal.SIGKILL)

    def test_cached_installer_is_verified_before_use(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            cache = root / "cache"
            cache.mkdir()
            (cache / "setup.exe").write_bytes(b"MZverified-installer")
            (root / "artifacts.json").write_text(json.dumps({"mt4": {
                "url": "https://vendor.invalid/setup.exe", "filename": "setup.exe",
                "sha256": hashlib.sha256(b"MZverified-installer").hexdigest(),
            }}))
            with patch.object(runtime, "SOURCE", root):
                runtime.download(cache)
                (cache / "setup.exe").write_bytes(b"MZchanged-installer")
                with self.assertRaisesRegex(runtime.RuntimeFailure, "artifact_checksum_mismatch"):
                    runtime.download(cache)
            self.assertEqual((root / "artifacts/setup.exe").read_bytes(), b"MZverified-installer")
            self.assertFalse((root / "artifacts/setup.exe.partial").exists())

    def test_timeout_stops_the_command_process_group(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)
            command = [sys.executable, "-c", (
                "import os, pathlib, signal, subprocess, sys, time; "
                "signal.signal(signal.SIGTERM, signal.SIG_IGN); "
                "child = subprocess.Popen([sys.executable, '-c', "
                "'import signal,time; signal.signal(signal.SIGTERM, signal.SIG_IGN); time.sleep(60)']); "
                f"pathlib.Path({str(path / 'pid')!r}).write_text(str(os.getpgrp())); "
                "time.sleep(60)"
            )]
            with patch.object(runtime, "DIAGNOSTICS", path):
                with self.assertRaisesRegex(runtime.RuntimeFailure, "timeout_test_timeout"):
                    runtime.run(command, "timeout_test", 1)
            group = int((path / "pid").read_text())
            deadline = time.monotonic() + 3
            while time.monotonic() < deadline:
                try:
                    os.killpg(group, 0)
                except ProcessLookupError:
                    break
                time.sleep(0.05)
            else:
                self.fail("Timed-out command left processes in its group")

    def test_diagnostics_are_bounded_without_blocking_the_command(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)
            with patch.object(runtime, "DIAGNOSTICS", path):
                code = runtime.run(
                    [sys.executable, "-c", "import os; os.write(1, b'x' * (2 * 1024 * 1024))"],
                    "large_output", 10,
                )
            self.assertEqual(code, 0)
            self.assertEqual((path / "large_output.log").stat().st_size, runtime.MAX_DIAGNOSTIC_BYTES)

    def test_archive_escape_is_rejected_before_any_extraction(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)
            archive_path = path / "unsafe.zip"
            with zipfile.ZipFile(archive_path, "w") as archive:
                archive.writestr("safe.txt", "safe")
                archive.writestr("../outside.txt", "unsafe")
            with self.assertRaisesRegex(runtime.RuntimeFailure, "unsafe_archive_path"):
                runtime.extract(archive_path, path / "destination")
            self.assertFalse((path / "destination").exists())
            self.assertFalse((path / "outside.txt").exists())

    def test_archive_expansion_limit_is_enforced(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)
            archive_path = path / "large.zip"
            with zipfile.ZipFile(archive_path, "w", zipfile.ZIP_DEFLATED) as archive:
                archive.writestr("large.txt", b"x" * 1024)
            with patch.object(runtime, "MAX_EXTRACTED_BYTES", 512):
                with self.assertRaisesRegex(runtime.RuntimeFailure, "archive_too_large"):
                    runtime.extract(archive_path, path / "destination")

    def test_stale_or_future_health_is_not_healthy(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "health.json"
            with patch.object(runtime, "HEALTH", path), patch.object(runtime, "display_ready", return_value=True):
                for observed_at in [time.monotonic() - 15, time.monotonic() + 15]:
                    with self.subTest(observed_at=observed_at):
                        path.write_text(json.dumps({"pid": os.getpid(), "observed_at": observed_at}))
                        self.assertEqual(runtime.health(), 1)
                path.write_text(json.dumps({"pid": os.getpid(), "observed_at": time.monotonic()}))
                self.assertEqual(runtime.health(), 0)

    def test_setup_does_not_overwrite_an_existing_provisioned_runtime(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)
            state = path / "provisioned.json"
            state.write_text('{"platform":"mt4"}')
            with patch.object(runtime, "DATA", path):
                with self.assertRaisesRegex(runtime.RuntimeFailure, "runtime_already_provisioned"):
                    runtime.setup("mt4")
            self.assertEqual(state.read_text(), '{"platform":"mt4"}')

    def test_unusable_normal_wine_execution_prevents_terminal_installation(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            prefix = root / "wine"
            prefix.mkdir()

            def command(command: list[str], name: str, _timeout: int, *,
                        env: dict[str, str] | None = None) -> int:
                if name == "wineboot":
                    self.assertEqual(command, ["wineboot", "--update"])
                    self.assertIsNotNone(env)
                    assert env is not None
                    self.assertEqual(env["WINEARCH"], "wow64")
                    self.assertEqual(env["WINEBOOTSTRAPMODE"], "1")
                if name in ("wine64_check", "wine32_check"):
                    self.assertIsNotNone(env)
                    assert env is not None
                    self.assertEqual(env["WINEARCH"], "win64")
                    self.assertNotIn("WINEBOOTSTRAPMODE", env)
                if name == "wine32_check":
                    raise runtime.RuntimeFailure("wine32_check_failed")
                return 0

            with (patch.object(runtime, "DATA", root), patch.object(runtime, "PREFIX", prefix),
                  patch.object(runtime, "run", side_effect=command) as commands,
                  patch.object(runtime.subprocess, "run")):
                with self.assertRaisesRegex(runtime.RuntimeFailure, "wine32_check_failed"):
                    runtime.setup("mt5")
                self.assertFalse(any(call.args[1] == "terminal_install" for call in commands.call_args_list))
                self.assertFalse((root / "provisioned.json").exists())

    def test_webview2_registration_requires_matching_payloads_under_normal_wine(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            version = "157.0.4341.42"
            location = "C:\\Program Files (x86)\\Microsoft\\EdgeWebView\\Application\\" + version
            payloads = root / "wine/drive_c/Program Files (x86)/Microsoft/EdgeWebView/Application" / version
            payloads.mkdir(parents=True)
            values = {"pv": version, "EBWebView": location}

            def registry(command: list[str], name: str, _timeout: int, *, check: bool,
                         env: dict[str, str]) -> int:
                self.assertFalse(check)
                self.assertEqual(env["WINEARCH"], "win64")
                self.assertNotIn("WINEBOOTSTRAPMODE", env)
                self.assertEqual(command[:3], ["wine", "C:\\windows\\system32\\reg.exe", "query"])
                value = command[-1]
                (root / f"{name}.log").write_text(f"\n    {value}    REG_SZ    {values[value]}\n")
                return 0

            with (patch.object(runtime, "PREFIX", root / "wine"), patch.object(runtime, "DIAGNOSTICS", root),
                  patch.object(runtime, "run", side_effect=registry)):
                self.assertIsNone(runtime.webview2_version())
                (payloads / "msedgewebview2.exe").write_bytes(b"MZbrowser")
                (payloads / "msedge.dll").write_bytes(b"MZlibrary")
                self.assertEqual(runtime.webview2_version(), version)
                for value in ["0.0.0.0", "not-a-version", "157.0.4341.43"]:
                    with self.subTest(version=value):
                        values["pv"] = value
                        self.assertIsNone(runtime.webview2_version())
                values["pv"] = version
                for value in ["Z:\\outside\\" + version, "C:\\other\\" + version,
                              "C:\\..\\Microsoft\\EdgeWebView\\Application\\" + version]:
                    with self.subTest(location=value):
                        values["EBWebView"] = value
                        self.assertIsNone(runtime.webview2_version())
                values["EBWebView"] = location
                (payloads / "msedge.dll").write_bytes(b"damaged-library")
                self.assertIsNone(runtime.webview2_version())
                (payloads / "msedge.dll").unlink()
                (root / "outside.dll").write_bytes(b"MZlibrary")
                (payloads / "msedge.dll").symlink_to(root / "outside.dll")
                self.assertIsNone(runtime.webview2_version())

    def test_existing_webview2_registration_skips_the_bootstrapper(self) -> None:
        with (patch.object(runtime, "webview2_version", return_value="157.0.4341.42"),
              patch.object(runtime, "run") as commands, patch.object(runtime, "event") as events):
            runtime.ensure_webview2()
            commands.assert_not_called()
            self.assertFalse(events.call_args.kwargs["browser_execution_verified"])

    def test_windows_console_probe_events_survive_wrapping_without_publishing_private_fields(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            diagnostic = Path(directory) / "probe.log"
            diagnostic.write_text(
                '\x1b[?25l{"event": "connector.mt5.package_imported", "python": "3.14.10", "python_bits": \n\n'
                '64, "numpy": "2.5.4", "metatrader5": "5.0.6231", "password": "private-fixture"}\x1b[K\x1b[?25h\n'
                '{"event": "connector.mt5.ipc_probe", "initialized": false, "error_code": -10005}\n'
                '{"event": "connector.mt5.unknown", "password": "private-fixture"}\n'
                '{"event": "connector.mt5.package_imported", broken\n'
            )
            with patch.object(runtime, "event") as events:
                runtime.publish_probe_events(diagnostic)
            self.assertEqual(events.call_count, 2)
            self.assertEqual(events.call_args_list[0].args, ("connector.mt5.package_imported", "mt5"))
            self.assertEqual(events.call_args_list[0].kwargs,
                             {"python": "3.14.10", "python_bits": 64, "numpy": "2.5.4", "metatrader5": "5.0.6231"})
            self.assertEqual(events.call_args_list[1].kwargs, {"initialized": False, "error_code": -10005})

    def test_hung_webview2_bootstrapper_continues_only_after_verified_registration(self) -> None:
        for version in [None, "157.0.4341.42"]:
            with self.subTest(registered=version is not None):
                def command(_command: list[str], name: str, _timeout: int, *, check: bool = True) -> int:
                    if name == "webview2_install":
                        raise runtime.RuntimeFailure("webview2_install_timeout")
                    return 0

                with (patch.object(runtime, "webview2_version", side_effect=[None, version]),
                      patch.object(runtime, "run", side_effect=command),
                      patch.object(runtime.subprocess, "run") as cleanup,
                      patch.object(runtime, "event") as events):
                    if version is None:
                        with self.assertRaisesRegex(runtime.RuntimeFailure, "webview2_install_timeout"):
                            runtime.ensure_webview2()
                        events.assert_not_called()
                    else:
                        runtime.ensure_webview2()
                        self.assertTrue(events.call_args.kwargs["installer_timed_out"])
                        self.assertFalse(events.call_args.kwargs["browser_execution_verified"])
                    self.assertEqual(cleanup.call_args.args[0], ["wineserver", "--kill"])

    def test_successful_webview2_installer_exit_is_not_registration_evidence(self) -> None:
        with (patch.object(runtime, "webview2_version", return_value=None),
              patch.object(runtime, "run", return_value=0), patch.object(runtime.subprocess, "run")):
            with self.assertRaisesRegex(runtime.RuntimeFailure, "webview2_registration_not_verified"):
                runtime.ensure_webview2()

    def test_failed_windows_package_import_prevents_mt5_provisioning(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            terminal = root / "wine/drive_c/Terminal"
            terminal.mkdir(parents=True)
            for name in ("terminal64.exe", "MetaEditor64.exe"):
                (terminal / name).write_bytes(b"MZpayload")
            source = root / "source"
            (source / "artifacts").mkdir(parents=True)
            (source / "artifacts/manifest.json").write_text("{}")
            for name in ("mt5_probe.py", "mt5_collect.py", "account_request.py"):
                (source / name).write_text("")

            def command(_command: list[str], name: str, _timeout: int, *, check: bool = True) -> int:
                if name == "mt5_package_check":
                    (root / f"{name}.log").write_text(
                        '{"event": "connector.mt5.package_import_failed", "error_code": "windows_package_import_failed"}\n'
                    )
                    return 1
                return 0

            with (patch.object(runtime, "DATA", root), patch.object(runtime, "PREFIX", root / "wine"),
                  patch.object(runtime, "SOURCE", source), patch.object(runtime, "DIAGNOSTICS", root),
                  patch.object(runtime, "initialize_wine"), patch.object(runtime, "ensure_webview2"),
                  patch.object(runtime, "extract"), patch.object(runtime, "run", side_effect=command) as commands,
                  patch.object(runtime.subprocess, "run")):
                with self.assertRaisesRegex(runtime.RuntimeFailure, "windows_package_import_failed"):
                    runtime.setup("mt5")
                self.assertFalse((root / "provisioned.json").exists())
                self.assertFalse(any(call.args[1] == "terminal_install" for call in commands.call_args_list))

    def test_installer_exit_is_not_provisioning_evidence_without_payloads_and_tools(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            terminal = root / "wine/drive_c/Terminal"
            terminal.mkdir(parents=True)
            source = root / "source"
            (source / "artifacts").mkdir(parents=True)
            (source / "artifacts/manifest.json").write_text("{}")
            with (patch.object(runtime, "DATA", root), patch.object(runtime, "PREFIX", root / "wine"),
                  patch.object(runtime, "SOURCE", source), patch.object(runtime, "initialize_wine"),
                  patch.object(runtime, "run", return_value=1),
                  patch.object(runtime.subprocess, "run"), patch.object(runtime, "install_tools") as tools):
                with self.assertRaisesRegex(runtime.RuntimeFailure, "terminal_payload_missing"):
                    runtime.setup("mt4")
                tools.assert_not_called()
                self.assertFalse((root / "provisioned.json").exists())
                (terminal / "terminal.exe").write_bytes(b"MZterminal")
                (terminal / "metaeditor.exe").write_bytes(b"MZeditor")
                tools.side_effect = runtime.RuntimeFailure("mt4_compile_diagnostics")
                with self.assertRaisesRegex(runtime.RuntimeFailure, "mt4_compile_diagnostics"):
                    runtime.setup("mt4")
                self.assertFalse((root / "provisioned.json").exists())
                tools.side_effect = None
                runtime.setup("mt4")
                self.assertEqual(json.loads((root / "provisioned.json").read_text())["platform"], "mt4")

    def test_fresh_mt4_request_copies_only_allowlisted_catalog_and_request_settings(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            request = self.mt4_fixture(root)
            terminal = root / "wine/drive_c/Terminal"
            for name in ("accounts.ini", "terminal.ini", "community.ini", "other-server.srv"):
                (terminal / "config" / name).write_text("cached-credentials-not-for-copy")
            (terminal / "profiles").mkdir()
            (terminal / "profiles/default.chr").write_text("cached-profile")
            (terminal / "config/fixture-server.srv").write_bytes(b"legacy-server")
            private = root / "private"
            private.mkdir(mode=0o700)
            with patch.object(runtime, "PREFIX", root / "wine"):
                startup = runtime.prepare_mt4_request(private, request, "a" * 32)
            self.assertEqual({path.name for path in (private / "config").iterdir()}, {"servers.ini", "fixture-server.srv"})
            self.assertEqual((private / "config/servers.ini").read_bytes(), (terminal / "config/servers.ini").read_bytes())
            self.assertFalse((private / "profiles").exists())
            self.assertEqual({path.name for path in (private / "MQL4/Scripts").iterdir()}, {"ReadOnlyCollector.ex4"})
            self.assertEqual(startup.stat().st_mode & 0o777, 0o600)
            text = startup.read_bytes().decode("ascii")
            self.assertIn("Password=fixture-password\n", text)
            self.assertIn("Login=123\n", text)
            self.assertIn("Server=fixture-server\nSymbol=EURUSD\nPeriod=M1\n", text)
            self.assertIn("ExpertsEnable=false\n", text)
            self.assertIn("ExpertsTrades=false\n", text)
            self.assertIn("ExpertsDllImport=false\nExpertsExpImport=false\n", text)
            self.assertIn("Script=ReadOnlyCollector\nScriptParameters=collector.set\n", text)
            preset = (private / "MQL4/Presets/collector.set").read_bytes().decode("ascii")
            self.assertEqual(preset, (
                "ExpectedLogin=123\nExpectedServer=fixture-server\n"
                f"HistoryFrom={request['from_epoch']}\nHistoryTo={request['to_epoch']}\nAttemptId={'a' * 32}\n"
            ))

    def test_mt4_server_catalog_guard_rejects_missing_malformed_and_symlinked_files(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            request = self.mt4_fixture(root)
            catalog = root / "wine/drive_c/Terminal/config/servers.ini"
            destination = root / "destination"
            destination.mkdir()
            with patch.object(runtime, "PREFIX", root / "wine"):
                catalog.unlink()
                with self.assertRaisesRegex(runtime.RuntimeFailure, "broker_server_definition_required"):
                    runtime.copy_mt4_servers(destination, request["server"])
                catalog.write_bytes(b"cached-credentials")
                with self.assertRaisesRegex(runtime.RuntimeFailure, "invalid_broker_server_catalog"):
                    runtime.copy_mt4_servers(destination, request["server"])
                catalog.unlink()
                (root / "private-credentials").write_text("cached-credentials")
                catalog.symlink_to(root / "private-credentials")
                with self.assertRaisesRegex(runtime.RuntimeFailure, "invalid_broker_server_definition"):
                    runtime.copy_mt4_servers(destination, request["server"])
                self.assertEqual(list(destination.iterdir()), [])

    def test_mt4_cancellation_and_terminal_exit_clean_files_then_restore_idle_without_replacing_results(self) -> None:
        for failure in ("source_request_cancelled", "runtime_process_exited"):
            with self.subTest(failure=failure), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                request = self.mt4_fixture(root)
                (root / "results").mkdir()
                accepted = root / "results/source-read.json"
                accepted.write_text("previous-accepted-source")
                queue = root / "queue.json"
                queue.write_text(json.dumps({"attempt_id": "a" * 32, "deadline": time.monotonic() + 5,
                                             "client_pid": os.getpid()}))
                health = root / "health.json"
                processes: list[subprocess.Popen[bytes]] = []
                request_paths: list[Path] = []
                request_processes: list[subprocess.Popen[bytes]] = []

                def start(_platform: str, children: list[subprocess.Popen[bytes]], executable: Path | None = None,
                          config: str = "C:\\Terminal\\runtime.ini") -> None:
                    if executable is not None:
                        request_paths.append(executable.parent)
                        if failure == "source_request_cancelled":
                            queue.write_text(json.dumps({"attempt_id": "b" * 32}))
                    elif request_paths:
                        self.assertFalse(request_paths[-1].exists())
                        self.assertIsNotNone(request_processes[-1].poll())
                    process = subprocess.Popen([sys.executable, "-c", "import time;time.sleep(30)"], start_new_session=True)
                    children.append(process)
                    if executable is not None:
                        request_processes.append(process)
                        if failure == "runtime_process_exited":
                            process.kill()
                            process.wait(timeout=3)

                try:
                    with (patch.object(runtime, "DATA", root), patch.object(runtime, "PREFIX", root / "wine"),
                          patch.object(runtime, "MT4_REQUEST", queue), patch.object(runtime, "HEALTH", health),
                          patch.object(runtime, "DIAGNOSTICS", root),
                          patch.object(runtime, "load_private_request", return_value=request),
                          patch.object(runtime, "start_terminal", side_effect=start),
                          patch.object(runtime.subprocess, "run"), patch.object(runtime, "run", return_value=0)):
                        start("mt4", processes)
                        runtime.supervise_mt4_request(processes)
                    response = queue.with_name("metatrader-mt4-response-" + "a" * 32 + ".json")
                    self.assertEqual(json.loads(response.read_text())["error_code"], failure)
                    self.assertEqual(accepted.read_text(), "previous-accepted-source")
                    self.assertEqual(len(processes), 1)
                    self.assertIsNone(processes[0].poll())
                    self.assertFalse(request_paths[0].exists())
                finally:
                    for process in processes:
                        runtime.stop_process(process)

    def test_failed_mt4_terminal_cleanup_retains_private_files_and_does_not_restore_idle(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            request = self.mt4_fixture(root)
            queue = root / "queue.json"
            queue.write_text(json.dumps({"attempt_id": "a" * 32, "deadline": time.monotonic() + 5,
                                         "client_pid": os.getpid()}))
            paths: list[Path] = []

            def start(_platform: str, _children: list[subprocess.Popen[bytes]], executable: Path | None = None,
                      config: str = "C:\\Terminal\\runtime.ini") -> None:
                if executable is not None:
                    paths.append(executable.parent)
                    queue.write_text(json.dumps({"attempt_id": "b" * 32}))

            with (patch.object(runtime, "DATA", root), patch.object(runtime, "PREFIX", root / "wine"),
                  patch.object(runtime, "MT4_REQUEST", queue),
                  patch.object(runtime, "load_private_request", return_value=request),
                  patch.object(runtime, "start_terminal", side_effect=start) as starts,
                  patch.object(runtime, "stop_terminal", side_effect=[None, runtime.RuntimeFailure("terminal_cleanup_timeout")]),
                  patch.object(runtime, "refresh_health")):
                try:
                    with self.assertRaisesRegex(runtime.RuntimeFailure, "terminal_cleanup_timeout"):
                        runtime.supervise_mt4_request([])
                    self.assertTrue((paths[0] / "request.ini").exists())
                    self.assertEqual(starts.call_count, 1)
                finally:
                    if paths:
                        shutil.rmtree(paths[0])

    def test_private_request_rejects_public_or_symlinked_credentials(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            request = root / "request.json"
            end = int(time.time())
            request.write_text(json.dumps({"login": "123", "server": "fixture-server", "password": "fixture-password",
                                           "from_epoch": str(end - 3600), "to_epoch": str(end)}))
            request.chmod(0o644)
            with self.assertRaisesRegex(RequestFailure, "private_account_request_required"):
                load_private_request(request, "mt5")
            request.chmod(0o600)
            self.assertEqual(load_private_request(request, "mt5")["login"], "123")
            (root / "link.json").symlink_to(request)
            with self.assertRaisesRegex(RequestFailure, "private_account_request_required"):
                load_private_request(root / "link.json", "mt5")

    def test_only_fresh_requested_login_investor_evidence_is_accepted(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            log = root / "terminal.log"
            log.write_text("0\t12:00\t'123': trading has been disabled - investor mode\r\n", encoding="utf-16")
            cursor = journal_cursor(root)
            self.assertFalse(investor_authorized(root, "123", cursor))
            with log.open("ab") as output:
                output.write("0\t12:01\t'456': trading has been disabled - investor mode\r\n".encode("utf-16-le"))
            self.assertFalse(investor_authorized(root, "123", cursor))
            with log.open("ab") as output:
                output.write("0\t12:02\t'123': trading has been disabled - investor mode\r\n".encode("utf-16-le"))
            self.assertTrue(investor_authorized(root, "123", cursor))

    def test_source_attempt_and_identity_failures_preserve_last_accepted_read(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            results = root / "results"
            results.mkdir()
            accepted = results / "source-read.json"
            accepted.write_text("previous-accepted-source")
            end = int(time.time())
            request = {"login": "123", "server": "fixture-server", "password": "fixture-password",
                       "from_epoch": str(end - 3600), "to_epoch": str(end)}
            result = {"schema_version": 1, "platform": "mt5", "attempt_id": "a" * 32,
                      "account": {"login": "123", "server": "fixture-server", "trade_allowed": False},
                      "read_only_verified": False,
                      "history_coverage": {"from_epoch": request["from_epoch"], "to_epoch": request["to_epoch"],
                                           "status": "broker_returned_window", "full_account_history_verified": False},
                      "orders": [], "positions": [], "history_orders": [], "history_deals": []}
            output = root / "read.json"
            for changes, code in [({"attempt_id": "b" * 32}, "source_attempt_mismatch"),
                                  ({"account": {"login": "456", "server": "fixture-server", "trade_allowed": False}},
                                   "account_identity_mismatch"), ({"positions": None}, "source_read_incomplete")]:
                with self.subTest(code=code):
                    output.write_text(json.dumps({**result, **changes}))
                    with self.assertRaisesRegex(ResultFailure, code):
                        read_source(output, "mt5", request, "a" * 32)
                    self.assertEqual(accepted.read_text(), "previous-accepted-source")
            output.write_text(json.dumps(result))
            snapshot = read_source(output, "mt5", request, "a" * 32)
            self.assertEqual(accept_source(snapshot, results), {"orders": 0, "positions": 0, "history_orders": 0, "history_deals": 0})
            self.assertTrue(json.loads(accepted.read_text())["read_only_verified"])
            self.assertEqual(accepted.stat().st_mode & 0o777, 0o600)


if __name__ == "__main__":
    unittest.main()
