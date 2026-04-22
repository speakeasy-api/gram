FROM rust:1.88-bookworm AS runner-builder

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

FROM debian:bookworm-slim AS lightpanda
ARG TARGETARCH
ARG LIGHTPANDA_SHA256_AMD64=87a757deaccc6cf4d69690c7f97422dcd7c99e58f462091d6b3afb6c23d4c58d
ARG LIGHTPANDA_SHA256_ARM64=01871c43298026fcc164de3c2c284e8c8c5da0098b9926642bb2e4f0e6b3c0f0
RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates curl \
  && rm -rf /var/lib/apt/lists/*
RUN case "${TARGETARCH:-amd64}" in \
      amd64) asset=lightpanda-x86_64-linux; sha=$LIGHTPANDA_SHA256_AMD64 ;; \
      arm64) asset=lightpanda-aarch64-linux; sha=$LIGHTPANDA_SHA256_ARM64 ;; \
      *) echo "unsupported arch: ${TARGETARCH}" >&2; exit 1 ;; \
    esac \
 && curl -fsSL -o /lightpanda "https://github.com/lightpanda-io/browser/releases/download/nightly/${asset}" \
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
    procps \
  && rm -rf /var/lib/apt/lists/*

COPY --from=runner-builder /src/agents/runner/target/release/gram-assistant-runner /usr/local/bin/gram-assistant-runner
COPY --from=bun /usr/local/bin/bun /usr/local/bin/bun
COPY --from=lightpanda /lightpanda /usr/local/bin/lightpanda
COPY agents/runtime-image/lightpanda-supervise.sh /usr/local/bin/lightpanda-supervise
COPY agents/runtime-image/init.sh /init

COPY agents/runtime-image/sandbox/package.json /tmp/bun-sandbox/package.json
COPY agents/runtime-image/sandbox/browser.ts /tmp/bun-sandbox/browser.ts
COPY --from=sandbox-deps /sandbox/node_modules /tmp/bun-sandbox/node_modules

RUN chmod 0755 /init /usr/local/bin/gram-assistant-runner /usr/local/bin/bun /usr/local/bin/lightpanda /usr/local/bin/lightpanda-supervise

ENTRYPOINT ["/init"]
