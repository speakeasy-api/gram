#!/usr/bin/env node
// A byte pipe between a TCP port and a Unix socket, so a jailed agent with no
// network of its own can reach exactly the ports it is given (see README.md).
//
//   bridge.mjs unix-to-tcp <socket> <port> <group-id>
//       Outside the jail, as root: listen on <socket>, owned by root and
//       <group-id> with mode 0660, and connect each client to 127.0.0.1:<port>.
//   bridge.mjs tcp-to-unix <port> <socket>
//       Inside the jail: listen on 127.0.0.1:<port> and [::1]:<port>, and
//       connect each client to <socket>.
//
// It reads no data, so it has nothing to parse and nothing to get wrong.

import fs from "node:fs";
import net from "node:net";

function pipe(client, connectTo) {
  const upstream = net.connect(connectTo);
  upstream.on("error", () => client.destroy());
  client.on("error", () => upstream.destroy());
  client.pipe(upstream);
  upstream.pipe(client);
}

const [mode, a, b, c] = process.argv.slice(2);

function portOf(value) {
  const port = Number(value);
  if (!Number.isInteger(port) || port < 1 || port > 65535) {
    console.error(`bridge: bad port ${value}`);
    process.exit(2);
  }
  return port;
}

if (mode === "unix-to-tcp" && a && b && c) {
  const port = portOf(b);
  fs.rmSync(a, { force: true });
  const server = net.createServer((client) =>
    pipe(client, { host: "127.0.0.1", port }),
  );
  // Create the socket unreachable, then open it to the run users' group.
  const umask = process.umask(0o177);
  server.listen(a, () => {
    process.umask(umask);
    fs.chownSync(a, 0, Number(c));
    fs.chmodSync(a, 0o660);
    console.log(`bridge ready: ${a} -> 127.0.0.1:${port}`);
  });
} else if (mode === "tcp-to-unix" && a && b) {
  const port = portOf(a);
  for (const host of ["127.0.0.1", "::1"]) {
    const server = net.createServer((client) => pipe(client, { path: b }));
    // IPv6 may be off in the namespace; IPv4 alone is enough.
    server.on("error", (err) => {
      if (host === "::1") return;
      throw err;
    });
    server.listen(port, host);
  }
} else {
  console.error(
    "usage: bridge.mjs unix-to-tcp <socket> <port> <group-id> | tcp-to-unix <port> <socket>",
  );
  process.exit(2);
}
