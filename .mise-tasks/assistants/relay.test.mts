import assert from "node:assert/strict";
import { once } from "node:events";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import net from "node:net";
import { networkInterfaces, tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { $ } from "zx";
import {
  bridgeGateway,
  callbackPort,
  createRelay,
  parseAddress,
  relayTarget,
} from "../start/assistant-relay.mts";

async function listen(server: net.Server) {
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const address = server.address() as net.AddressInfo;
  return { host: address.address, port: address.port };
}

async function exchange(
  address: { host: string; port: number },
  payload: Buffer,
) {
  const client = net.createConnection(address);
  const chunks: Buffer[] = [];
  client.end(payload);
  for await (const chunk of client) chunks.push(chunk);
  return Buffer.concat(chunks);
}

for (const scenario of [
  "non-local gateway",
  "explicit override",
  "daemon unavailable",
  "network unavailable",
  "missing IPv4 gateway",
  "malformed response",
  "Docker Desktop with interface override",
  "Docker Desktop with loopback override",
  "daemon unavailable with override",
]) {
  const explicit = scenario === "explicit override";
  test(
    `Docker discovery: ${scenario} ${explicit ? "fails" : "keeps the daemon alive but inactive"}`,
    { timeout: 10_000 },
    async (t) => {
      const dir = mkdtempSync(join(tmpdir(), "assistant-relay-test-"));
      t.after(() => rmSync(dir, { recursive: true, force: true }));
      writeFileSync(
        join(dir, "docker"),
        `#!${process.execPath}
const scenario = ${JSON.stringify(scenario)};
if (scenario.startsWith('daemon unavailable') || (scenario === 'network unavailable' && process.argv[2] === 'network')) {
  console.error('Docker probe unavailable');
  process.exit(1);
}
if (scenario === 'malformed response') {
  console.log('not JSON');
  process.exit(0);
}
if (process.argv[2] === 'info') {
  console.log(JSON.stringify({ OperatingSystem: scenario.startsWith('Docker Desktop') ? 'Docker Desktop' : 'Linux' }));
} else {
  console.log(JSON.stringify([{ IPAM: { Config: scenario === 'missing IPv4 gateway' ? [] : [{ Gateway: '192.0.2.1' }] } }]));
}
`,
        { mode: 0o755 },
      );
      const task = fileURLToPath(
        new URL("../start/assistant-relay.mts", import.meta.url),
      );
      const interfaceIP =
        Object.values(networkInterfaces())
          .flat()
          .find((address) => address?.family === "IPv4" && !address.internal)
          ?.address ?? "192.0.2.1";
      const override =
        scenario === "Docker Desktop with loopback override"
          ? "127.0.0.1"
          : scenario.endsWith("with interface override") ||
              scenario === "daemon unavailable with override"
            ? interfaceIP
            : explicit
              ? "192.0.2.1"
              : "";
      const command = $({
        quiet: true,
        nothrow: true,
        env: {
          ...process.env,
          PATH: `${dir}:${process.env.PATH}`,
          usage_server_address: "127.0.0.1:23456",
          usage_server_url: "https://localhost:23456",
          usage_runtime_server_url: "",
          usage_listen_host: override,
        },
      })`${process.execPath} ${task}`;
      t.after(async () => {
        if (command.stage === "running") await command.kill("SIGTERM");
        await command;
      });
      if (explicit) {
        const result = await command;
        assert.equal(result.exitCode, 1);
        assert.match(result.stderr, /requires a Docker gateway IP/);
      } else {
        await new Promise<void>((resolve, reject) => {
          let output = "";
          command.stdout.on("data", (chunk) => {
            output += chunk.toString();
            if (output.includes("Assistant relay ready:")) resolve();
          });
          command.then(
            () => reject(new Error("relay exited before readiness")),
            reject,
          );
        });
        await command.kill("SIGTERM");
        const result = await command;
        assert.equal(result.exitCode, 0);
        if (scenario.startsWith("Docker Desktop")) {
          assert.match(
            result.stdout,
            /Assistant relay ready: Docker Desktop host forwarding/,
          );
          assert.equal(result.stderr, "");
          return;
        }
        assert.match(result.stdout, /Assistant relay ready: inactive:/);
        assert.match(result.stderr, /Assistant relay inactive/);
        assert.match(result.stderr, /GRAM_ASSISTANT_RUNTIME_SERVER_URL/);
        if (scenario !== "non-local gateway") {
          assert.match(result.stderr, /Docker gateway discovery failed/);
        }
      }
    },
  );
}

test("API address parsing supports loopback, wildcard, and bracketed IPv6", () => {
  assert.deepEqual(parseAddress("127.0.0.1:8080"), {
    host: "127.0.0.1",
    port: 8080,
  });
  assert.deepEqual(parseAddress(":8080"), { host: "", port: 8080 });
  assert.deepEqual(parseAddress("[::1]:8080"), { host: "::1", port: 8080 });
  for (const invalid of [
    "localhost",
    "::1:8080",
    "localhost:0",
    "localhost:65536",
  ]) {
    assert.throws(() => parseAddress(invalid), /Invalid API listener/);
  }
});

test("callback port follows the runtime rewrite and explicit overrides", () => {
  assert.equal(callbackPort("https://localhost:23456"), 23456);
  assert.equal(callbackPort("https://dev.example.ts.net:23456"), 23456);
  assert.equal(callbackPort("https://localhost"), 443);
  assert.equal(callbackPort("http://localhost"), 80);
  assert.equal(
    callbackPort(
      "https://localhost:23456",
      "https://host.docker.internal:23457",
    ),
    23457,
  );
  assert.equal(
    callbackPort("https://localhost:23456", "https://dev.example.ts.net:23456"),
    null,
  );
  assert.throws(() => callbackPort("ftp://localhost"), /HTTP or HTTPS/);
});

test("relay avoids occupied listeners and forwards to loopback or Tailnet overrides", () => {
  const listener = { host: "192.0.2.1", port: 23456 };
  for (const host of ["", "0.0.0.0", "::", listener.host]) {
    assert.equal(relayTarget({ host, port: 23456 }, listener), null);
  }
  for (const host of ["127.0.0.1", "::1", "100.64.0.1"]) {
    assert.deepEqual(relayTarget({ host, port: 23456 }, listener), {
      host,
      port: 23456,
    });
  }
  assert.deepEqual(relayTarget({ host: "0.0.0.0", port: 23457 }, listener), {
    host: "127.0.0.1",
    port: 23457,
  });
});

test("gateway discovery uses the default bridge IPv4 gateway", () => {
  assert.equal(
    bridgeGateway({
      IPAM: { Config: [{ Gateway: "fd00::1" }, { Gateway: "192.0.2.1" }] },
    }),
    "192.0.2.1",
  );
  assert.throws(() => bridgeGateway({}), /no IPv4 gateway/);
});

test(
  "relay preserves concurrent binary streams larger than socket buffers",
  { timeout: 10_000 },
  async (t) => {
    const upstream = net.createServer((socket) => socket.pipe(socket));
    const target = await listen(upstream);
    t.after(() => upstream.close());
    const relay = createRelay(target);
    const address = await listen(relay.server);
    t.after(() => relay.close());
    const payload = Buffer.alloc(2 * 1024 * 1024);
    for (let i = 0; i < payload.length; i++) payload[i] = i % 256;
    const results = await Promise.all([
      exchange(address, payload),
      exchange(address, payload),
    ]);
    for (const result of results) assert.deepEqual(result, payload);
  },
);

test(
  "client half-close still permits a complete upstream response",
  { timeout: 10_000 },
  async (t) => {
    const response = Buffer.alloc(2 * 1024 * 1024, "response");
    const upstream = net.createServer({ allowHalfOpen: true }, (socket) => {
      socket.resume();
      socket.on("end", () => socket.end(response));
    });
    const target = await listen(upstream);
    t.after(() => upstream.close());
    const relay = createRelay(target);
    const address = await listen(relay.server);
    t.after(() => relay.close());
    assert.deepEqual(await exchange(address, Buffer.from("request")), response);
  },
);

test(
  "relay survives unavailable upstream and reconnects after server startup",
  { timeout: 10_000 },
  async (t) => {
    const upstream = net.createServer((socket) => socket.pipe(socket));
    const target = await listen(upstream);
    await new Promise<void>((resolve) => upstream.close(() => resolve()));
    const relay = createRelay(target);
    const address = await listen(relay.server);
    t.after(() => relay.close());
    const client = net.createConnection(address);
    client.on("error", () => {});
    client.resume();
    await new Promise<void>((resolve) => client.once("close", () => resolve()));
    upstream.listen(target);
    await once(upstream, "listening");
    t.after(() => upstream.close());
    assert.equal(
      (await exchange(address, Buffer.from("recovered"))).toString(),
      "recovered",
    );
  },
);

test(
  "shutdown closes active streams and releases the listener",
  { timeout: 10_000 },
  async (t) => {
    const upstream = net.createServer();
    const target = await listen(upstream);
    t.after(() => upstream.close());
    const relay = createRelay(target);
    const address = await listen(relay.server);
    const connected = once(upstream, "connection");
    const client = net.createConnection(address);
    client.resume();
    const [targetSocket] = (await connected) as [net.Socket];
    targetSocket.resume();
    const clientClosed = once(client, "close");
    const targetClosed = once(targetSocket, "close");
    await relay.close();
    await Promise.all([clientClosed, targetClosed]);
    assert.equal(relay.server.listening, false);
  },
);
