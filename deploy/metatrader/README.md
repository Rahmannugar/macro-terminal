# MetaTrader compatibility runtime

This isolated runtime prepares MT4 and MT5 for managed, read-only connectivity
testing on an existing Linux x86-64 host. It is not the Journal API, account
onboarding, a durable importer, or proof of broker connectivity.

The Go application continues to own product authorization, persistence,
normalization, and background work. These containers contain Wine and the
terminal-specific integration tools only.

## Isolation

- Separate Compose project, networks, and data volumes for MT4 and MT5.
- Non-root runtime, read-only image filesystem, dropped capabilities, and no
  production mounts, environment file, Docker socket, or production network.
- One CPU per terminal, 1.5 GiB maximum RAM for MT4 and 2 GiB for MT5, with swap
  disabled for both. These are experiment limits, not verified fleet capacity.
- Desktop ports bind to host loopback only. No desktop server starts by default.
  Private administration must use an authenticated viewer and an SSH tunnel.
- Terminal automated trading and DLL imports are disabled. These settings are
  safeguards, not proof of investor authorization.

Wine profiles separate state; container/process boundaries provide isolation.
Do not mount another account's data into either terminal. A container is not an
equivalent security boundary to a separately managed virtual machine.

## Installation

Run from this directory, not the production Compose directory:

```sh
docker buildx build --builder macro-terminal-connectivity \
  --build-context vendor-artifacts=/opt/macro-terminal-connectivity/artifacts \
  --load --tag macro-terminal-metatrader-wine:0.1.0 .
docker compose -f compose.yml run --rm --no-deps mt4 setup
docker compose -f compose.yml run --rm --no-deps mt5 setup
docker compose -f compose.yml up --detach --no-build mt4 mt5
docker compose -f compose.yml ps
docker compose -f compose.yml exec mt5 python3 /opt/metatrader/runtime.py probe
docker compose -f compose.yml exec mt5 python3 /opt/metatrader/runtime.py probe --ipc
```

The image uses pinned WineHQ staging, Windows CPython, NumPy, and the official
MetaTrader5 wheel. Windows Python and the MT5 terminal share one Wine profile;
native Linux Python cannot load the Windows adapter. Published Python/PyPI
checksums are verified before extraction. The terminal installers are also
checksum-locked. The named artifact context can contain official installers
downloaded directly from MetaQuotes and transferred through SSH if the vendor
refuses downloads from the VPS; unverified caches are rejected. Missing cached
artifacts use the recorded HTTPS source. Terminal installation can download a
newer platform build; record the actual terminal build separately.

Installation is explicit. Starting an unprovisioned runtime fails rather than
automatically installing software. Prefix initialization and repair use Wine's
bootstrap mode only for that operation; both 64-bit and 32-bit commands must
then pass under normal settings before installation continues. Existing
profiles are preserved. Installer diagnostics stay in the private
data volume; application logs expose bounded outcomes, not their raw output.
No host package upgrade, Windows installation, production restart, or host
reboot is required by these commands.

MT5 setup detects WebView2 through normal-Wine runtime registration and matching
browser executable/DLL payloads. An already registered runtime is not reinstalled.
A timed-out bootstrapper is stopped before registration is checked again;
neither installer exit nor file presence alone permits continuation. This proves
installed registration, not browser usability. Existing terminal/editor payloads
can be reused on a retry; editor filename case follows Windows semantics.
Payload checks and real Windows Python/NumPy/adapter imports must pass before
provisioning is recorded. Local terminal startup is a separate acceptance check.

The MT5 probe has separate package-import and terminal-IPC checks. It does not
read accounts, orders, or history. Account-free initialization is not guaranteed
by the vendor; an offline IPC timeout is not proof that authenticated operation
will fail. The MT4 probe compiles using MetaEditor and reads only connection
status and record counts when explicitly run. Its output never asserts verified
read-only access or full history.

## Verification boundary

Record terminal startup/build, package import, IPC outcome, compiled MT4 probe,
resource use, container restart, and administrator-session independence first.
Then configure the owner's existing test accounts privately and run
the bounded source readers. Confirm platform/server/login and
broker-enforced investor access before accepting source data. A disabled
AutoTrading button, trade-disabled account flag, successful package import, or
passing container healthcheck alone is not connectivity acceptance.

## Private source reads

The host administrator prepares private directories without creating account
credentials, then enters missing credentials in a private interactive SSH
terminal. Prompts do not echo. The configuration command requires an actual
interactive terminal; an agent cannot supply credentials that are not stored.
Do not bypass that check, use demo credentials, or paste credentials or account
output into chat, command-line arguments, environment variables, or logs.

```sh
python3 private_account.py prepare
python3 private_account.py configure mt4
python3 private_account.py configure mt5
```

Once private requests exist, the agent runs collection and bounded validation
without the owner repeating these commands. Check only request presence before
asking for configuration; preserve existing private requests. Missing requests
return `private_account_request_required` before broker authentication. They are
not evidence of a failed login or incompatible adapter.

An authorized agent may open a private interactive SSH terminal and start
configuration for the owner to enter details. Do not capture or read that
terminal's contents. A successful terminal launch is not proof of completed
configuration; check request presence after the owner's safe confirmation.

For the telemetry-enabled VPS experiment, run from
`/opt/macro-terminal-connectivity/source`:

```sh
docker compose --env-file /dev/null -f compose.yml -f compose.telemetry.yml exec -T mt4 python3 /opt/metatrader/runtime.py collect
docker compose --env-file /dev/null -f compose.yml -f compose.telemetry.yml exec -T mt5 python3 /opt/metatrader/runtime.py collect
```

MT4 requires a modern binary `config/servers.ini` catalog or the requested
legacy `.srv` definition and a valid broker chart symbol. Modern catalogs are
opaque; connected server/login checks, not a plaintext catalog search, verify
the requested identity. The private desktop may be
used to discover/install that server; no desktop server runs by default. Launch
`runtime.py desktop` through Compose exec and use only the loopback-bound port
through an SSH tunnel. Display its password only in a private interactive
terminal with `private_account.py desktop-password mt4` (or `mt5`).

MT4 collection is handled by the runtime supervisor. Each request runs a fresh
portable terminal in private temporary storage, copying only the binary,
read-only collector, and allowlisted server catalog/definition. Saved accounts,
passwords, charts, and expert profiles are not copied. This prevents cached
credentials from overriding the requested password. The supervisor stops that
terminal before removing its temporary files and restores the idle terminal.
Failed terminal cleanup stops the supervisor rather than deleting files while
Wine may still be using them or reporting a display-only runtime as healthy.
MT5 explicitly supplies credentials to adapter initialization and login.

MT4 startup configuration and script presets use ASCII without a byte-order
mark. MT4 server, password, and chart-symbol values must be printable ASCII;
other encodings are not verified and are rejected rather than silently changed.
MT5 request values retain Unicode support.

Both readers require connected identity, disabled account trading, a matching
attempt identifier, and fresh terminal investor-authorization evidence. Only
then is the private `/data/results/source-read.json` replaced atomically. Failed
or partial reads leave the last accepted result untouched. These files are
connectivity evidence, not Journal persistence or synchronization checkpoints.
Reads are bounded to 120 seconds, 10,000 records per source kind, seven-day
history windows, and 16 MiB output. They are sequential, not transactional.
MT4 reports terminal-loaded history only and does not claim a verified broker
timezone or full account history. Refresh an existing private history window
with `private_account.py refresh-window mt4` (or `mt5`).

## Connector telemetry

`compose.telemetry.yml` sends connector OTLP/HTTP logs, metrics, and traces to a
resource-limited relay on their isolated Docker networks. Only the relay also
joins the existing `macro-terminal-prod_default` network and forwards to
`otel-collector:4318`; terminals never join that network or receive a New Relic
key. The relay has no published host port or host networking. Production
configuration and host firewall rules remain unchanged. Use both Compose files
for start/exec/stop when this overlay is enabled; no relay environment file is
required. The existing production Collector network must already be present.
Packet forwarding is disabled in the relay. Its private `:8888/metrics` endpoint
reports receiver and upstream-export outcomes for operational verification.

Signals are `connector.operations`, `connector.operation.duration`,
`connector.operation.active`, `connector.source.records`, and
`connector.runtime.healthy`, plus bounded operation spans and correlated
lifecycle/source events. Health means supervised local processes and display,
not authenticated broker connectivity. Source counts record accepted reads,
not durable imports. Platform, operation, outcome, and source kind are bounded
metric dimensions. Credentials, broker/account identities, source records, and
raw exceptions are omitted. Export failures produce a rate-limited safe event;
shutdown has an eight-second bound. Existing production host/Docker metrics
provide host and container resource observations. Live New Relic receipt is a
separate check; successful local OTLP receipt is not proof of it.

Run focused tests with the locked telemetry dependencies available:

```sh
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest -v \
  test_runtime.py test_collectors.py test_telemetry.py
```

Stop only this experiment without deleting its data:

```sh
docker compose -f compose.yml down
```

Do not add `--volumes`, prune unrelated images, or reboot the shared production
host as an experiment cleanup shortcut.
