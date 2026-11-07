import argparse
import json
import struct
import sys


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--ipc", action="store_true")
    arguments = parser.parse_args()
    try:
        import numpy
        import MetaTrader5 as mt5
    except (ImportError, OSError):
        print(json.dumps({
            "event": "connector.mt5.package_import_failed",
            "error_code": "windows_package_import_failed",
        }), flush=True)
        return 1

    print(json.dumps({
        "event": "connector.mt5.package_imported",
        "python": sys.version.split()[0],
        "python_bits": struct.calcsize("P") * 8,
        "numpy": numpy.__version__,
        "metatrader5": mt5.__version__,
    }), flush=True)
    if not arguments.ipc:
        return 0
    try:
        initialized = mt5.initialize("C:\\Terminal\\terminal64.exe", timeout=60000, portable=True)
        error = mt5.last_error()
        print(json.dumps({
            "event": "connector.mt5.ipc_probe",
            "initialized": bool(initialized),
            "error_code": int(error[0]),
            "account_configured": False,
            "broker_connectivity_verified": False,
        }), flush=True)
        if initialized:
            version = mt5.version()
            print(json.dumps({
                "event": "connector.mt5.terminal_version",
                "terminal_version": int(version[0]),
                "terminal_build": int(version[1]),
            }), flush=True)
        return 0 if initialized else 2
    except Exception:
        print(json.dumps({
            "event": "connector.mt5.ipc_probe_failed",
            "error_code": "adapter_operation_failed",
        }), flush=True)
        return 1
    finally:
        mt5.shutdown()


if __name__ == "__main__":
    sys.exit(main())
