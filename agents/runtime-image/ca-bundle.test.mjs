import { afterAll, beforeAll, expect, test } from "bun:test";
import { spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { bunBundle, fingerprint, verifyBundle } from "./ca-bundle.mjs";

const directory = mkdtempSync(join(tmpdir(), "ca-bundle-test-"));
const certFile = join(directory, "localhost.pem");
const keyFile = join(directory, "localhost.key");
const systemBundle = readFileSync("/etc/ssl/certs/ca-certificates.crt", "utf8");
const allowedFile = join(directory, "allowed.pem");
const rejectedFile = join(directory, "rejected.pem");
let certificate;

beforeAll(() => {
  const generated = spawnSync(
    "openssl",
    [
      "req",
      "-x509",
      "-newkey",
      "ec",
      "-pkeyopt",
      "ec_paramgen_curve:P-256",
      "-nodes",
      "-days",
      "1",
      "-subj",
      "/CN=localhost",
      "-addext",
      "subjectAltName=DNS:localhost,IP:127.0.0.1",
      "-keyout",
      keyFile,
      "-out",
      certFile,
    ],
    { encoding: "utf8" },
  );
  expect(generated.status, generated.stderr).toBe(0);
  certificate = readFileSync(certFile, "utf8");
  writeFileSync(allowedFile, systemBundle);
  writeFileSync(
    rejectedFile,
    bunBundle(systemBundle, [certificate], new Set([fingerprint(certificate)])),
  );
});

afterAll(() => rmSync(directory, { recursive: true, force: true }));

test("checks certificate fingerprints independently of distro filenames", () => {
  const renamedFile = join(directory, "renamed-by-distribution.crt");
  writeFileSync(renamedFile, systemBundle + certificate);
  expect(() =>
    verifyBundle(
      readFileSync(renamedFile, "utf8"),
      new Set([fingerprint(certificate)]),
    ),
  ).toThrow("distrusted root");
});

test("refuses an empty trust store", () => {
  expect(() => verifyBundle("")).toThrow("no certificates");
});

// Executed in fresh Bun processes: root stores are cached process-wide.
// NODE_EXTRA_CA_CERTS supplies a root through Bun's additive trust path, just
// as bundled roots are added after OpenSSL loads SSL_CERT_FILE.
async function request(api, url) {
  const parsed = new URL(url);
  if (api === "fetch") {
    const response = await fetch(url, { signal: AbortSignal.timeout(3000) });
    await response.text();
  } else if (api === "https") {
    await new Promise((resolve, reject) => {
      require("node:https")
        .get(url, (response) => {
          response.resume();
          response.on("end", resolve);
        })
        .on("error", reject);
    });
  } else if (api === "tls") {
    await new Promise((resolve, reject) => {
      const socket = require("node:tls").connect({
        host: parsed.hostname,
        port: Number(parsed.port),
        servername: "localhost",
      });
      socket.on("secureConnect", () => {
        socket.destroy();
        resolve();
      });
      socket.on("error", reject);
    });
  } else if (api === "bun-connect") {
    await new Promise((resolve, reject) => {
      Bun.connect({
        hostname: parsed.hostname,
        port: Number(parsed.port),
        tls: { serverName: "localhost", rejectUnauthorized: true },
        socket: {
          // The low-level API exposes the verification result in handshake.
          handshake(socket, success, error) {
            socket.end();
            if (error) reject(error);
            else if (!success) reject(new Error("TLS handshake failed"));
            else resolve();
          },
          data() {},
          error(_socket, error) {
            reject(error);
          },
        },
      }).catch(reject);
    });
  } else if (api === "websocket") {
    await new Promise((resolve, reject) => {
      const socket = new WebSocket(url.replace("https:", "wss:"));
      socket.onopen = () => {
        socket.close();
        resolve();
      };
      socket.onerror = () =>
        reject(new Error("WebSocket TLS connection failed"));
    });
  }
}

test.each(["fetch", "https", "tls", "bun-connect", "websocket"])(
  "%s honors explicit distrust despite an additive CA",
  async (api) => {
    const server = Bun.serve({
      hostname: "127.0.0.1",
      port: 0,
      tls: { cert: certificate, key: readFileSync(keyFile) },
      fetch(req, server) {
        if (server.upgrade(req)) return;
        return new Response("trusted");
      },
      websocket: { message() {} },
    });
    try {
      for (const [file, expectedExit] of [
        [allowedFile, 0],
        [rejectedFile, 1],
      ]) {
        const script = `(${request.toString()})(${JSON.stringify(api)}, ${JSON.stringify(server.url.toString())})
          .then(() => console.log("trusted"))
          .catch(e => { console.error(e.code ?? e.cause?.code ?? e.message); process.exitCode = 1; });`;
        const child = Bun.spawn([process.execPath, "-e", script], {
          env: {
            ...process.env,
            SSL_CERT_FILE: file,
            SSL_CERT_DIR: directory,
            NODE_EXTRA_CA_CERTS: certFile,
          },
          stdout: "pipe",
          stderr: "pipe",
        });
        const [code, stdout, stderr] = await Promise.all([
          child.exited,
          new Response(child.stdout).text(),
          new Response(child.stderr).text(),
        ]);
        expect(code, `${api}: ${stdout} ${stderr}`).toBe(expectedExit);
        if (expectedExit === 0) expect(stdout.trim()).toBe("trusted");
        else expect(stderr).toMatch(/CERT|ISSUER|TLS|certificate/i);
      }
    } finally {
      server.stop(true);
    }
  },
);
