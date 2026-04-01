# Stage 1: Build DefenseClaw Go gateway + extract policy files
FROM golang:1.25-bookworm AS dc-builder
RUN apt-get update && apt-get install -y --no-install-recommends git
WORKDIR /build
RUN git clone https://github.com/cisco-ai-defense/defenseclaw.git .
RUN go build -o /defenseclaw-gateway ./cmd/defenseclaw

# Stage 2: Install openshell-sandbox binary from NVIDIA OCI image (~6MB)
FROM python:3.12-slim-bookworm AS openshell-installer
RUN apt-get update && apt-get install -y --no-install-recommends curl ca-certificates
COPY --from=dc-builder /build/scripts/install-openshell-sandbox.sh /tmp/install.sh
RUN OPENSHELL_INSTALL_DIR=/opt/bin bash /tmp/install.sh

# Stage 3: Runtime
FROM python:3.12-slim-bookworm

RUN apt-get update && apt-get install -y --no-install-recommends \
    curl ca-certificates iptables iproute2 dnsutils procps \
    && rm -rf /var/lib/apt/lists/*

# Node.js 22 for OpenClaw
RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/*

# OpenClaw CLI
RUN npm install -g openclaw

# DefenseClaw Go gateway binary
COPY --from=dc-builder /defenseclaw-gateway /usr/local/bin/defenseclaw-gateway

# OpenShell sandbox binary (creates namespaces, veth pair, Landlock, seccomp)
COPY --from=openshell-installer /opt/bin/openshell-sandbox /usr/local/bin/openshell-sandbox

# OpenShell OPA policy rules (Rego — from DefenseClaw)
COPY --from=dc-builder /build/policies/openshell/default.rego /etc/defenseclaw/openshell-policy.rego

# DefenseClaw Python CLI (reuse the clone from dc-builder instead of fetching again)
COPY --from=dc-builder /build /tmp/defenseclaw
RUN pip install --no-cache-dir /tmp/defenseclaw && rm -rf /tmp/defenseclaw

# Create sandbox user (openshell-sandbox drops privileges to this user)
RUN useradd -m -s /bin/bash sandbox \
    && mkdir -p /home/sandbox/.openclaw \
    && chown -R sandbox:sandbox /home/sandbox

# POC scripts
COPY start.py /poc/start.py
COPY start-openclaw.sh /poc/start-openclaw.sh
RUN chmod +x /poc/start.py /poc/start-openclaw.sh

WORKDIR /poc
EXPOSE 18789

CMD ["python3", "/poc/start.py"]
