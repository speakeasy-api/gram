FROM rust:1.95-bookworm AS runner-builder

WORKDIR /src
COPY agents/runner/Cargo.toml agents/runner/Cargo.lock ./agents/runner/
COPY agents/runner/src ./agents/runner/src
RUN cargo build --manifest-path ./agents/runner/Cargo.toml --release --bin gram-assistant-runner

FROM oven/bun:1.2.15 AS bun

FROM oven/bun:1.2.15 AS sandbox-deps
WORKDIR /sandbox
COPY agents/runtime-image/sandbox/package.json ./
RUN bun install --production

# Workaround for oven-sh/bun#9911: playwright-core's bundled `ws` shim is
# missing 'upgrade'/'unexpected-response' events on bun, which breaks
# chromium.connectOverCDP. Force playwright to use the userland `ws` package
# when running on bun.
#
# Removal criteria: drop this patch only after a bun release lands BOTH
#   1. oven-sh/bun#28828 (http: client 'upgrade' event) — open as of writing
#   2. a follow-up fix for the http:// form of connectOverCDP, which #28828
#      explicitly does not address (see PR description). Our browser.ts
#      connects via http://127.0.0.1:9222, which hits this exact path.
# Track via oven-sh/bun#9911 for the umbrella issue.
#
# The grep guard fails the build loudly if a future playwright bump moves
# this line, so we don't silently regress.
RUN target=node_modules/playwright-core/lib/utilsBundle.js \
 && sed -i "s|const ws = exports.ws = require('./utilsBundleImpl').ws;|const ws = exports.ws = 'Bun' in globalThis ? require('ws') : require('./utilsBundleImpl').ws;|" "$target" \
 && grep -q "'Bun' in globalThis ? require('ws')" "$target"

FROM debian:bookworm-slim AS workdir-template
# 256 MiB hard cap on assistant workspace + bundled sandbox helpers.
ARG WORKDIR_IMAGE_SIZE=268435456
RUN apt-get update \
  && apt-get install -y --no-install-recommends e2fsprogs util-linux \
  && rm -rf /var/lib/apt/lists/*
WORKDIR /staging
COPY agents/runtime-image/sandbox/package.json /staging/package.json
COPY agents/runtime-image/sandbox/browser.ts /staging/browser.ts
COPY --from=sandbox-deps /sandbox/node_modules /staging/node_modules
RUN fallocate -l "${WORKDIR_IMAGE_SIZE}" /workdir-template.ext4 \
 && mke2fs -t ext4 -d /staging -F /workdir-template.ext4

FROM debian:bookworm-slim AS lightpanda
ARG TARGETARCH
ARG LIGHTPANDA_VERSION=0.2.8
ARG LIGHTPANDA_SHA256_AMD64=8e3a5e04cf508699990a78a0a8686ea3398912cd9891fda90513429b89230300
ARG LIGHTPANDA_SHA256_ARM64=9f54f2cc31b0dadd867ba06ecce59f8aa59f7876394798e97882aea680b5ad19
RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates curl \
  && rm -rf /var/lib/apt/lists/*
RUN case "${TARGETARCH:-amd64}" in \
      amd64) asset=lightpanda-x86_64-linux; sha=$LIGHTPANDA_SHA256_AMD64 ;; \
      arm64) asset=lightpanda-aarch64-linux; sha=$LIGHTPANDA_SHA256_ARM64 ;; \
      *) echo "unsupported arch: ${TARGETARCH}" >&2; exit 1 ;; \
    esac \
 && curl -fsSL -o /lightpanda "https://github.com/lightpanda-io/browser/releases/download/${LIGHTPANDA_VERSION}/${asset}" \
 && echo "${sha}  /lightpanda" | sha256sum -c - \
 && chmod 0755 /lightpanda

FROM debian:bookworm-slim

RUN apt-get update \
  && apt-get install -y --no-install-recommends \
    bash \
    ca-certificates \
    curl \
    iproute2 \
    libcurl4 \
    libssl3 \
    mount \
    procps \
    util-linux \
  && rm -rf /var/lib/apt/lists/*

COPY --from=runner-builder /src/agents/runner/target/release/gram-assistant-runner /usr/local/bin/gram-assistant-runner
COPY --from=bun /usr/local/bin/bun /usr/local/bin/bun
COPY --from=lightpanda /lightpanda /usr/local/bin/lightpanda
COPY agents/runtime-image/lightpanda-supervise.sh /usr/local/bin/lightpanda-supervise
COPY agents/runtime-image/init.sh /init

# Fixed-size ext4 prepopulated with the sandbox helpers; init.sh loop-mounts
# it at /var/lib/gram-assistant/work so the assistant has a persistent
# workdir whose disk usage is hard-capped by the image size.
COPY --from=workdir-template /workdir-template.ext4 /usr/share/gram/workdir-template.ext4

RUN chmod 0755 /init /usr/local/bin/gram-assistant-runner /usr/local/bin/bun /usr/local/bin/lightpanda /usr/local/bin/lightpanda-supervise

ENTRYPOINT ["/init"]
