import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import {
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  readdirSync,
  readlinkSync,
  rmSync,
  symlinkSync,
  statSync,
  utimesSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test, type TestContext } from "node:test";
import { fileURLToPath } from "node:url";
import {
  publish,
  restartUnderStackLock,
  splitPEM,
  type CertPair,
} from "./tailscale.mts";

const task = fileURLToPath(new URL("./tailscale.mts", import.meta.url));
const domain = "dev-machine.example.ts.net";

function tempDir(t: TestContext): string {
  const dir = mkdtempSync(join(tmpdir(), "tls-tailscale-test-"));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  return dir;
}

function selfSigned(t: TestContext): CertPair {
  const dir = tempDir(t);
  execFileSync(
    "openssl",
    [
      "req",
      "-x509",
      "-newkey",
      "ec",
      "-pkeyopt",
      "ec_paramgen_curve:P-256",
      "-nodes",
      "-subj",
      `/CN=${domain}`,
      "-days",
      "1",
      "-keyout",
      join(dir, "key.pem"),
      "-out",
      join(dir, "cert.pem"),
    ],
    { stdio: "ignore" },
  );
  return {
    cert: readFileSync(join(dir, "cert.pem"), "utf8"),
    key: readFileSync(join(dir, "key.pem"), "utf8"),
  };
}

test("splitPEM separates the certificate chain from the private key", (t) => {
  const pair = selfSigned(t);
  assert.deepEqual(splitPEM(pair.cert + pair.key), pair);
});

test("splitPEM rejects output without a matching key", (t) => {
  const pair = selfSigned(t);
  const other = selfSigned(t);
  assert.throws(() => splitPEM(pair.cert), /no private key/);
  assert.throws(() => splitPEM(pair.key), /no certificate/);
  assert.throws(() => splitPEM(pair.cert + other.key), /do not match/);
});

test("publish links a new pair and is a no-op when it is current", (t) => {
  const sslDir = join(tempDir(t), "ssl");
  const pair = selfSigned(t);
  assert.equal(publish(sslDir, pair), true);
  const target = readlinkSync(join(sslDir, "tailscale"));
  assert.match(target, /^tailscale-/);
  assert.equal(
    readFileSync(join(sslDir, "tailscale", "cert.pem"), "utf8"),
    pair.cert,
  );
  assert.equal(
    readFileSync(join(sslDir, "tailscale", "key.pem"), "utf8"),
    pair.key,
  );
  assert.equal(
    statSync(join(sslDir, "tailscale", "key.pem")).mode & 0o777,
    0o600,
  );

  assert.equal(publish(sslDir, pair), false);
  assert.equal(readlinkSync(join(sslDir, "tailscale")), target);
  assert.equal(
    readdirSync(sslDir).filter((name) => !name.startsWith(".")).length,
    2,
  );
});

test("publish swaps the link on renewal and only removes stale versions", (t) => {
  const sslDir = join(tempDir(t), "ssl");
  const first = selfSigned(t);
  publish(sslDir, first);
  const old = readlinkSync(join(sslDir, "tailscale"));
  // A version another run created moments ago but has not linked yet.
  mkdirSync(join(sslDir, "tailscale-inflight"));
  const hourAgo = new Date(Date.now() - 2 * 60 * 60 * 1000);
  utimesSync(join(sslDir, old), hourAgo, hourAgo);

  const renewed = selfSigned(t);
  assert.equal(publish(sslDir, renewed), true);
  const current = readlinkSync(join(sslDir, "tailscale"));
  assert.notEqual(current, old);
  assert.equal(
    readFileSync(join(sslDir, "tailscale", "cert.pem"), "utf8"),
    renewed.cert,
  );
  const entries = readdirSync(sslDir);
  assert.ok(!entries.includes(old));
  assert.ok(entries.includes("tailscale-inflight"));
  assert.ok(!entries.some((name) => name.startsWith(".tailscale-")));
});

function runTask(t: TestContext, stdout: string, exit = 0) {
  const root = tempDir(t);
  const bin = join(root, "bin");
  mkdirSync(bin);
  const calls = join(root, "calls.jsonl");
  writeFileSync(
    join(bin, "tailscale"),
    `#!${process.execPath}
require('node:fs').appendFileSync(process.env.CALLS, JSON.stringify({ args: process.argv.slice(2), cli: process.env.TAILSCALE_BE_CLI }) + '\\n');
process.stdout.write(process.env.OUTPUT);
process.stderr.write('tailscale stderr');
process.exit(${exit});
`,
    { mode: 0o755 },
  );
  const result = spawnSync(process.execPath, [task], {
    cwd: root,
    env: {
      PATH: `${bin}:/usr/bin:/bin`,
      CALLS: calls,
      OUTPUT: stdout,
      usage_domain: domain,
      usage_min_validity: "240h",
    },
    encoding: "utf8",
  });
  return { root, result, calls };
}

test("task requests the pair on stdout with the minimum validity", (t) => {
  const pair = selfSigned(t);
  const { root, result, calls } = runTask(t, pair.cert + pair.key);
  assert.equal(result.status, 0, result.stderr);
  const [call] = readFileSync(calls, "utf8")
    .trim()
    .split("\n")
    .map((line) => JSON.parse(line) as { args: string[]; cli: string });
  assert.deepEqual(call.args, [
    "cert",
    "--min-validity",
    "240h",
    "--cert-file",
    "-",
    "--key-file",
    "-",
    domain,
  ]);
  assert.equal(call.cli, "1");
  assert.equal(
    readFileSync(join(root, "local", "ssl", "tailscale", "cert.pem"), "utf8"),
    pair.cert,
  );
  assert.match(result.stdout, /Saved the Tailscale certificate/);
});

test("task failure explains how to enable certificates and leaves no files", (t) => {
  const { root, result } = runTask(t, "", 1);
  assert.equal(result.status, 1);
  assert.match(result.stderr, /tailscale stderr/);
  assert.match(result.stderr, /HTTPS certificates are enabled/);
  assert.throws(() => statSync(join(root, "local")));
});

function gitRepo(t: TestContext): { root: string; gitdir: string } {
  const root = tempDir(t);
  execFileSync("git", ["init", "-q", root]);
  return { root, gitdir: join(root, ".git") };
}

test("restart holds the stack lock while it runs and releases it", async (t) => {
  const { root, gitdir } = gitRepo(t);
  const lock = join(gitdir, "gram-stack-lock");
  let heldDuringRestart = "";
  const skipped = await restartUnderStackLock(root, async () => {
    heldDuringRestart = readlinkSync(lock);
  });
  assert.equal(skipped, undefined);
  assert.equal(heldDuringRestart, `${process.pid}:unpause`);
  assert.equal(existsSync(lock), false);
});

test("restart is skipped while another lifecycle operation owns the stack", async (t) => {
  const { root, gitdir } = gitRepo(t);
  const lock = join(gitdir, "gram-stack-lock");
  symlinkSync("12345:pause", lock);
  let restarted = false;
  const skipped = await restartUnderStackLock(root, async () => {
    restarted = true;
  });
  assert.match(skipped ?? "", /in progress/);
  assert.equal(restarted, false);
  assert.equal(readlinkSync(lock), "12345:pause");
});

test("restart is skipped on a paused stack and releases the lock", async (t) => {
  const { root, gitdir } = gitRepo(t);
  writeFileSync(join(gitdir, "gram-stack-paused"), "");
  let restarted = false;
  const skipped = await restartUnderStackLock(root, async () => {
    restarted = true;
  });
  assert.match(skipped ?? "", /paused/);
  assert.equal(restarted, false);
  assert.equal(existsSync(join(gitdir, "gram-stack-lock")), false);
});

test("restart reports why it skipped outside a git checkout", async (t) => {
  const root = tempDir(t);
  const skipped = await restartUnderStackLock(root, async () => {
    assert.fail("must not restart");
  });
  assert.match(skipped ?? "", /git directory/);
});
