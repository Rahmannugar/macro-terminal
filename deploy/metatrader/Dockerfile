FROM scratch AS vendor-artifacts

FROM ubuntu:24.04

ARG WINE_VERSION=11.21~noble-1

ENV DEBIAN_FRONTEND=noninteractive

RUN dpkg --add-architecture i386 \
    && apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl gnupg \
    && mkdir -p /etc/apt/keyrings \
    && curl --fail --silent --show-error --location \
        --retry 3 --connect-timeout 15 --max-time 60 \
        https://dl.winehq.org/wine-builds/winehq.key -o /tmp/winehq.key \
    && gpg --dearmor -o /etc/apt/keyrings/winehq.gpg /tmp/winehq.key \
    && rm /tmp/winehq.key \
    && echo 'deb [signed-by=/etc/apt/keyrings/winehq.gpg] https://dl.winehq.org/wine-builds/ubuntu/ noble main' \
        > /etc/apt/sources.list.d/winehq.list \
    && apt-get update \
    && apt-get install -y --install-recommends \
        "winehq-staging=${WINE_VERSION}" "wine-staging=${WINE_VERSION}" \
        "wine-staging-amd64=${WINE_VERSION}" "wine-staging-i386:i386=${WINE_VERSION}" \
        python3 xvfb x11-utils xdotool openbox x11vnc util-linux fonts-dejavu-core \
    && rm -rf /var/lib/apt/lists/*

COPY requirements.lock /opt/metatrader/requirements.lock
RUN apt-get update \
    && apt-get install -y --no-install-recommends python3-venv \
    && rm -rf /var/lib/apt/lists/* \
    && python3 -m venv /opt/telemetry \
    && /opt/telemetry/bin/pip install --disable-pip-version-check --no-cache-dir \
        --require-hashes --only-binary=:all: --platform any --abi none --implementation py \
        --target /opt/metatrader/telemetry-packages -r /opt/metatrader/requirements.lock

COPY artifacts.json /opt/metatrader/artifacts.json
COPY runtime.py telemetry.py account_request.py source_results.py mt5_probe.py mt5_collect.py /opt/metatrader/
COPY mt4 /opt/metatrader/mt4

RUN --mount=type=bind,from=vendor-artifacts,target=/opt/vendor-artifacts \
    python3 /opt/metatrader/runtime.py download --artifact-cache /opt/vendor-artifacts \
    && groupadd --gid 10001 terminal \
    && useradd --uid 10001 --gid 10001 --home-dir /data/home --no-create-home terminal \
    && mkdir -p /data \
    && chown terminal:terminal /data \
    && chmod 700 /data

ENV HOME=/data/home \
    WINEPREFIX=/data/wine \
    WINEARCH=win64 \
    WINEDEBUG=-all \
    WINEDLLOVERRIDES=mscoree,mshtml= \
    DISPLAY=:99 \
    XDG_RUNTIME_DIR=/tmp/runtime-terminal \
    PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PYTHONPATH=/opt/metatrader/telemetry-packages

USER terminal
WORKDIR /data

ENTRYPOINT ["python3", "/opt/metatrader/runtime.py"]
CMD ["serve"]
