import assert from "node:assert/strict";
import { once } from "node:events";
import net from "node:net";
import { test } from "node:test";
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
