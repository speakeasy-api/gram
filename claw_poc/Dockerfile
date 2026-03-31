# Stage 1: Build DefenseClaw Go gateway from GitHub
FROM golang:1.25-bookworm AS go-builder
RUN apt-get update && apt-get install -y --no-install-recommends git
WORKDIR /build
RUN git clone https://github.com/cisco-ai-defense/defenseclaw.git .
RUN go build -o /defenseclaw-gateway ./cmd/defenseclaw

# Stage 2: Runtime
FROM python:3.12-slim-bookworm

RUN apt-get update && apt-get install -y --no-install-recommends \
    curl ca-certificates git iptables dnsutils \
    && rm -rf /var/lib/apt/lists/*

# Node.js for OpenClaw
RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/*

# OpenClaw CLI
RUN npm install -g openclaw

# DefenseClaw Go binary
COPY --from=go-builder /defenseclaw-gateway /usr/local/bin/defenseclaw-gateway

# DefenseClaw Python CLI (from GitHub)
RUN pip install --no-cache-dir "defenseclaw @ git+https://github.com/cisco-ai-defense/defenseclaw.git"

# POC scripts
COPY poc.py /poc/poc.py
COPY start.sh /poc/start.sh
RUN chmod +x /poc/start.sh

WORKDIR /poc
EXPOSE 18789

CMD ["/poc/start.sh"]
