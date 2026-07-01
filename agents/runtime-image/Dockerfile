FROM rust:1.96-bookworm@sha256:5e2214abe154fe26e39f64488952e5c991eeed1d6d6da7cc8381ae83927f0cfc AS runner-builder

WORKDIR /src
COPY agents/runner/Cargo.toml agents/runner/Cargo.lock ./agents/runner/
COPY agents/runner/src ./agents/runner/src
RUN cargo build --manifest-path ./agents/runner/Cargo.toml --release --bin gram-assistant-runner

FROM oven/bun:1.3.14@sha256:e10577f0db68676a7024391c6e5cb4b879ebd17188ab750cf10024a6d700e5c4 AS bun

FROM oven/bun:1.3.14@sha256:e10577f0db68676a7024391c6e5cb4b879ebd17188ab750cf10024a6d700e5c4 AS sandbox-deps
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

FROM debian:bookworm-slim@sha256:96e378d7e6531ac9a15ad505478fcc2e69f371b10f5cdf87857c4b8188404716 AS lightpanda
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

FROM debian:bookworm-slim@sha256:96e378d7e6531ac9a15ad505478fcc2e69f371b10f5cdf87857c4b8188404716

RUN apt-get update \
  && apt-get install -y --no-install-recommends \
    bash \
    ca-certificates \
    curl \
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

# Read-only sandbox helpers (playwright-core + browser.ts) used by `bun_run` tool
COPY agents/runtime-image/sandbox/package.json /usr/share/gram/sandbox/package.json
COPY agents/runtime-image/sandbox/browser.ts /usr/share/gram/sandbox/browser.ts
COPY --from=sandbox-deps /sandbox/node_modules /usr/share/gram/sandbox/node_modules

RUN chmod 0755 /init /usr/local/bin/gram-assistant-runner /usr/local/bin/bun /usr/local/bin/lightpanda /usr/local/bin/lightpanda-supervise

ENTRYPOINT ["/init"]
