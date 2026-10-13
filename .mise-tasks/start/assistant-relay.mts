#!/usr/bin/env node

//MISE description="Relay local assistant callbacks from the Docker bridge to the API listener"
//MISE dir="{{ config_root }}"
//MISE hide=true
//USAGE flag "--server-address <address>" env="GRAM_SERVER_ADDRESS" required=#true help="API listener to forward connections to."
//USAGE flag "--server-url <url>" env="GRAM_SERVER_URL" required=#true help="API URL whose port the local runtime uses."
//USAGE flag "--runtime-server-url <url>" env="GRAM_ASSISTANT_RUNTIME_SERVER_URL" help="Explicit runtime callback URL, if configured."
//USAGE flag "--listen-host <host>" env="GRAM_ASSISTANT_RELAY_HOST" help="Override Docker's default bridge gateway when host-gateway is customized. Must be a local interface IP."

import { once } from "node:events";
import net from "node:net";
import { networkInterfaces } from "node:os";
import { $ } from "zx";

interface Address {
  host: string;
  port: number;
}

export function parseAddress(address: string): Address {
  const match = /^(?:\[([^\]]+)\]|([^:]*)):(\d+)$/.exec(address);
  const port = Number(match?.[3]);
  if (!match || !Number.isInteger(port) || port < 1 || port > 65535) {
    throw new Error(`Invalid API listener address: ${address}`);
  }
  return { host: match[1] ?? match[2] ?? "", port };
}

export function callbackPort(
  serverURL: string,
  runtimeURL?: string,
): number | null {
  const url = new URL(runtimeURL || serverURL);
  if (url.protocol !== "http:" && url.protocol !== "https:") {
    throw new Error("Assistant callback URL must use HTTP or HTTPS.");
  }
  // An explicit endpoint bypasses the local backend's host-gateway rewrite.
  if (runtimeURL && url.hostname !== "host.docker.internal") return null;
  return Number(url.port || (url.protocol === "https:" ? 443 : 80));
}

export function relayTarget(
  upstream: Address,
  listen: Address,
): Address | null {
  const wildcard = ["", "0.0.0.0", "::"].includes(upstream.host);
  if (
    upstream.port === listen.port &&
    (wildcard || upstream.host === listen.host)
  ) {
    // The API already accepts this endpoint; binding would conflict or recurse.
    return null;
  }
  if (!wildcard) return upstream;
  return {
    host: upstream.host === "::" ? "::1" : "127.0.0.1",
    port: upstream.port,
  };
}

export function bridgeGateway(network: {
  IPAM?: { Config?: { Gateway?: string }[] };
}): string {
  const gateway = network.IPAM?.Config?.find(
    (config) => net.isIP(config.Gateway ?? "") === 4,
  )?.Gateway;
  if (!gateway) throw new Error("Docker's default bridge has no IPv4 gateway.");
  return gateway;
}

export function createRelay(upstream: Address) {
  const sockets = new Set<net.Socket>();
  const server = net.createServer({ allowHalfOpen: true }, (client) => {
    const target = net.createConnection({ ...upstream, allowHalfOpen: true });
    sockets.add(client);
    sockets.add(target);
    const destroy = () => {
      client.destroy();
      target.destroy();
    };
    client.on("error", destroy);
    target.on("error", destroy);
    client.on("close", () => {
      sockets.delete(client);
      target.destroy();
    });
    target.on("close", () => {
      sockets.delete(target);
      // A graceful EOF has already queued client.end() through pipe. Let its
      // pending writes drain rather than truncating a large response.
      if (!target.readableEnded) client.destroy();
    });
    // Bound connection establishment, not stream lifetime: turns and SSE may
    // remain idle for a long time. Piping preserves TLS and backpressure.
    target.setTimeout(10_000, destroy);
    target.once("connect", () => target.setTimeout(0));
    client.pipe(target);
    target.pipe(client);
  });
  return {
    server,
    async close() {
      const closed = server.listening ? once(server, "close") : undefined;
      server.close();
      for (const socket of sockets) socket.destroy();
      await closed;
    },
  };
}

async function startRelay(): Promise<{
  reason: string;
  relay?: ReturnType<typeof createRelay>;
}> {
  const upstream = parseAddress(process.env["usage_server_address"] ?? "");
  const port = callbackPort(
    process.env["usage_server_url"] ?? "",
    process.env["usage_runtime_server_url"],
  );
  if (port === null) return { reason: "explicit runtime callback URL" };

  const hostOverride = process.env["usage_listen_host"];
  let host: string;
  try {
    const info = await $`docker info --format ${"{{json .}}"}`
      .quiet()
      .timeout(15_000)
      .json<{ OperatingSystem: string }>();
    // Desktop forwards host traffic itself, even when an override is supplied.
    if (/docker desktop/i.test(info.OperatingSystem)) {
      return { reason: "Docker Desktop host forwarding" };
    }
    host =
      hostOverride ||
      bridgeGateway(
        (
          await $`docker network inspect bridge`.quiet().timeout(15_000).json()
        )[0],
      );
  } catch (error) {
    console.warn(
      `Assistant relay inactive: Docker gateway discovery failed: ${error instanceof Error ? error.message : String(error)}. ` +
        "Restart assistant-relay after Docker is available, or configure a container-reachable GRAM_ASSISTANT_RUNTIME_SERVER_URL.",
    );
    return { reason: "inactive: Docker gateway discovery failed" };
  }

  const localIPs = Object.values(networkInterfaces()).flatMap(
    (addresses) => addresses?.map((address) => address.address) ?? [],
  );
  const isLocal = localIPs.includes(host);
  if (
    hostOverride &&
    (!net.isIP(host) || !isLocal || host === "::1" || host.startsWith("127."))
  ) {
    throw new Error(
      "Assistant relay requires a Docker gateway IP on a local non-loopback interface. " +
        "Check GRAM_ASSISTANT_RELAY_HOST or use an explicit reachable GRAM_ASSISTANT_RUNTIME_SERVER_URL.",
    );
  }
  if (!isLocal) {
    // CI and remote Docker may publish ports without sharing their bridge.
    console.warn(
      "Assistant relay inactive: Docker bridge gateway is not a local interface. " +
        "Local assistant callbacks require a container-reachable GRAM_ASSISTANT_RUNTIME_SERVER_URL " +
        "or a host-local gateway configured with GRAM_ASSISTANT_RELAY_HOST.",
    );
    return {
      reason: "inactive: Docker bridge gateway is not a local interface",
    };
  }

  const listen = { host, port };
  const target = relayTarget(upstream, listen);
  if (!target)
    return { reason: "API already listens on the Docker callback endpoint" };

  const relay = createRelay(target);
  relay.server.listen(listen);
  await once(relay.server, "listening");
  return {
    relay,
    reason: `${host}:${port} -> ${upstream.host}:${upstream.port}`,
  };
}

async function main() {
  const { relay, reason } = await startRelay();
  // Keep the supervised daemon alive when forwarding is unnecessary too.
  // A restart re-evaluates configuration after bind or Docker context changes.
  const keepAlive = setInterval(() => {}, 60_000);
  const stop = async () => {
    clearInterval(keepAlive);
    await relay?.close();
  };
  process.once("SIGINT", stop);
  process.once("SIGTERM", stop);
  console.log(`Assistant relay ready: ${reason}`);
}

if (import.meta.main) {
  main().catch((error) => {
    console.error(error instanceof Error ? error.message : String(error));
    process.exitCode = 1;
  });
}
