import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import {
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test, type TestContext } from "node:test";
import { fileURLToPath } from "node:url";

const task = fileURLToPath(new URL("./tls.sh", import.meta.url));

function fixture(t: TestContext) {
  const root = mkdtempSync(join(tmpdir(), "tls-names-test-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const bin = join(root, "bin");
  const ca = join(root, "ca");
  mkdirSync(bin);
  symlinkSync(process.execPath, join(bin, "node"));
  mkdirSync(ca);
  writeFileSync(join(ca, "rootCA.pem"), "synthetic CA");
  const calls = join(root, "calls.jsonl");
  const programs = {
    git: "console.log(process.env.FIXTURE);",
    uname: "console.log('Darwin');",
    mise: "",
    mkcert:
      "if (process.argv[2] === '-CAROOT') console.log(process.env.FIXTURE + '/ca');",
  };
  for (const [name, body] of Object.entries(programs)) {
    writeFileSync(
      join(bin, name),
      `#!${process.execPath}
require('node:fs').appendFileSync(process.env.CALLS, JSON.stringify({ name: ${JSON.stringify(name)}, args: process.argv.slice(2) }) + '\\n');
${body}
`,
      { mode: 0o755 },
    );
  }
  const runResult = (overrides: NodeJS.ProcessEnv = {}) => {
    const result = spawnSync("bash", [task], {
      cwd: root,
      env: {
        PATH: `${bin}:/usr/bin:/bin`,
        HOME: root,
        FIXTURE: root,
        CALLS: calls,
        GRAM_SERVER_URL: "https://localhost:8080",
        GRAM_SITE_URL: "https://localhost:5173",
        GRAM_DEVIDP_EXTERNAL_URL: "https://localhost:35291",
        ...overrides,
      },
      encoding: "utf8",
    });
    const recorded = readFileSync(calls, "utf8")
      .trim()
      .split("\n")
      .map((line) => JSON.parse(line) as { name: string; args: string[] });
    return { result, calls: recorded };
  };
  const run = (overrides: NodeJS.ProcessEnv = {}) => {
    const { result, calls } = runResult(overrides);
    assert.equal(result.status, 0, result.stderr);
    return calls;
  };
  return { run, runResult };
}

for (const [extras, expected] of [
  ["", []],
  [
    " dev-machine.example.ts.net,100.64.0.1,fd7a:115c:a1e0::1,localhost,100.64.0.1 ",
    ["dev-machine.example.ts.net", "100.64.0.1", "fd7a:115c:a1e0::1"],
  ],
] as const) {
  test(`mkcert includes deduplicated extra DNS names and IPs (${extras || "none"})`, (t) => {
    const f = fixture(t);
    const calls = f.run({ GRAM_SSL_EXTRA_HOSTS: extras });
    const generated = calls.find(
      (call) => call.name === "mkcert" && call.args[0] === "-cert-file",
    );
    assert.ok(generated);
    assert.deepEqual(generated.args.slice(4), [
      "localhost",
      "host.docker.internal",
      "127.0.0.1",
      "::1",
      ...expected,
    ]);
  });
}

for (const value of ["flag.example.com", ""]) {
  test(`explicit extra-hosts flag overrides the persisted environment value (${value || "empty"})`, (t) => {
    const f = fixture(t);
    const calls = f.run({
      GRAM_SSL_EXTRA_HOSTS: "env.example.com",
      usage_extra_hosts: value,
    });
    const generated = calls.find(
      (call) => call.name === "mkcert" && call.args[0] === "-cert-file",
    );
    assert.ok(generated);
    assert.deepEqual(generated.args.slice(4), [
      "localhost",
      "host.docker.internal",
      "127.0.0.1",
      "::1",
      ...(value ? [value] : []),
    ]);
  });
}

test("extra hosts do not enable TLS for HTTP-only endpoints", (t) => {
  const f = fixture(t);
  const calls = f.run({
    GRAM_SERVER_URL: "http://localhost:8080",
    GRAM_SITE_URL: "http://localhost:5173",
    GRAM_DEVIDP_EXTERNAL_URL: "http://localhost:35291",
    GRAM_SSL_EXTRA_HOSTS: "dev-machine.example.ts.net,100.64.0.1",
  });
  assert.equal(
    calls.some((call) => call.name === "mkcert"),
    false,
  );
});

for (const host of [
  "idp.example.ts.net",
  "100.64.0.1",
  "[fd7a:115c:a1e0::1]",
  "localhost.example.com",
  "127.0.0.1.example.com",
]) {
  for (const scheme of ["http", "https"]) {
    test(`non-loopback HTTP dev-idp URL fails before TLS changes (${host}, ${scheme} app URLs)`, (t) => {
      const f = fixture(t);
      const { result, calls } = f.runResult({
        GRAM_SERVER_URL: `${scheme}://localhost:8080`,
        GRAM_SITE_URL: `${scheme}://localhost:5173`,
        GRAM_DEVIDP_EXTERNAL_URL: `http://${host}:35291`,
      });
      assert.notEqual(result.status, 0);
      assert.match(result.stderr, /GRAM_DEVIDP_EXTERNAL_URL.*HTTPS.*loopback/);
      assert.equal(
        calls.some((call) => call.name === "mise" || call.name === "mkcert"),
        false,
      );
    });
  }
}

for (const host of [
  "localhost",
  "127.0.0.1",
  "127.255.255.255",
  "[::1]",
  "[::ffff:127.0.0.1]",
]) {
  test(`loopback HTTP dev-idp URL permits the all-HTTP opt-out (${host})`, (t) => {
    const f = fixture(t);
    const calls = f.run({
      GRAM_SERVER_URL: "http://localhost:8080",
      GRAM_SITE_URL: "http://localhost:5173",
      GRAM_DEVIDP_EXTERNAL_URL: `http://${host}:35291`,
    });
    assert.equal(
      calls.some((call) => call.name === "mkcert"),
      false,
    );
    assert.ok(
      calls.some(
        (call) =>
          call.name === "mise" && call.args.includes("GRAM_HTTP_SCHEME=http"),
      ),
    );
    for (const name of ["GRAM_SSL_CERT_FILE", "GRAM_SSL_KEY_FILE"]) {
      assert.ok(
        calls.some(
          (call) => call.name === "mise" && call.args.includes(`${name}=`),
        ),
      );
    }
  });
}

for (const url of [
  "not-a-url",
  "http:localhost:35291",
  "https://",
  "ftp://idp.example.ts.net",
  "https://user@idp.example.ts.net",
]) {
  test(`invalid dev-idp URL fails before TLS changes (${url})`, (t) => {
    const f = fixture(t);
    const { result, calls } = f.runResult({ GRAM_DEVIDP_EXTERNAL_URL: url });
    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /GRAM_DEVIDP_EXTERNAL_URL/);
    assert.equal(
      calls.some((call) => call.name === "mise" || call.name === "mkcert"),
      false,
    );
  });
}

for (const base of [
  "https://idp.example.ts.net:35291",
  "http://localhost:35291",
]) {
  for (const suffix of ["?x=1", "?", "#fragment", "#", "?x=1#fragment"]) {
    test(`dev-idp URL query or fragment fails before TLS changes (${base}${suffix})`, (t) => {
      const f = fixture(t);
      const { result, calls } = f.runResult({
        GRAM_DEVIDP_EXTERNAL_URL: `${base}/${suffix}`,
      });
      assert.notEqual(result.status, 0);
      assert.match(
        result.stderr,
        /GRAM_DEVIDP_EXTERNAL_URL.*query or fragment/,
      );
      assert.equal(
        calls.some((call) => call.name === "mise" || call.name === "mkcert"),
        false,
      );
    });
  }
}

test("dev-idp URL validation permits escaped path delimiters", (t) => {
  const f = fixture(t);
  f.run({
    GRAM_DEVIDP_EXTERNAL_URL:
      "https://idp.example.ts.net:35291/tenant%3Fname%23id",
  });
});

test("dev-idp HTTPS enables TLS when the server and dashboard URLs use HTTP", (t) => {
  const f = fixture(t);
  const { result, calls } = f.runResult({
    GRAM_SERVER_URL: "http://localhost:8080",
    GRAM_SITE_URL: "http://localhost:5173",
    GRAM_DEVIDP_EXTERNAL_URL: "https://idp.example.ts.net:35291",
  });
  assert.equal(result.status, 0, result.stderr);
  assert.match(
    result.stderr,
    /GRAM_DEVIDP_EXTERNAL_URL: https:\/\/idp\.example\.ts\.net:35291/,
  );
  assert.ok(
    calls.some(
      (call) =>
        call.name === "mise" && call.args.includes("GRAM_HTTP_SCHEME=https"),
    ),
  );
  const generated = calls.find(
    (call) => call.name === "mkcert" && call.args[0] === "-cert-file",
  );
  assert.ok(generated);
  assert.ok(generated.args.slice(4).includes("idp.example.ts.net"));
});

test("dev-idp HTTPS detection accepts case-insensitive URL schemes", (t) => {
  const f = fixture(t);
  const calls = f.run({
    GRAM_SERVER_URL: "http://localhost:8080",
    GRAM_SITE_URL: "http://localhost:5173",
    GRAM_DEVIDP_EXTERNAL_URL: "hTtPs://idp.example.ts.net:35291",
  });
  assert.ok(
    calls.some(
      (call) =>
        call.name === "mise" && call.args.includes("GRAM_HTTP_SCHEME=https"),
    ),
  );
});

test("mkcert includes a separately configured dev-idp hostname", (t) => {
  const f = fixture(t);
  const calls = f.run({
    GRAM_DEVIDP_EXTERNAL_URL: "https://idp.example.ts.net:35291",
  });
  const generated = calls.find(
    (call) => call.name === "mkcert" && call.args[0] === "-cert-file",
  );
  assert.ok(generated);
  assert.deepEqual(generated.args.slice(4), [
    "localhost",
    "idp.example.ts.net",
    "host.docker.internal",
    "127.0.0.1",
    "::1",
  ]);
});

for (const name of [
  "GRAM_SERVER_URL",
  "GRAM_SITE_URL",
  "GRAM_DEVIDP_EXTERNAL_URL",
]) {
  for (const host of ["::1", "fd7a:115c:a1e0::1"]) {
    test(`mkcert includes bare IPv6 SANs (${name}, ${host})`, (t) => {
      const f = fixture(t);
      const calls = f.run({ [name]: `https://[${host}]:35291/path` });
      const generated = calls.find(
        (call) => call.name === "mkcert" && call.args[0] === "-cert-file",
      );
      assert.ok(generated);
      const names = generated.args.slice(4);
      assert.ok(names.includes(host));
      assert.equal(
        names.some((name) => /[\[\]]/.test(name)),
        false,
      );
      assert.equal(names.filter((name) => name === host).length, 1);
    });
  }
}

test("TLS setup also works without a dev-idp URL", (t) => {
  const f = fixture(t);
  const calls = f.run({ GRAM_DEVIDP_EXTERNAL_URL: undefined });
  const generated = calls.find(
    (call) => call.name === "mkcert" && call.args[0] === "-cert-file",
  );
  assert.ok(generated);
  assert.deepEqual(generated.args.slice(4), [
    "localhost",
    "host.docker.internal",
    "127.0.0.1",
    "::1",
  ]);
});

for (const host of [
  "https://dev.example.ts.net:443",
  "dev.example.ts.net:443",
  "dev.example.ts.net/path",
  "dev machine.example.ts.net",
  "dev\tmachine.example.ts.net",
  "dev\nmachine.example.ts.net",
  "[fd7a:115c:a1e0::1]:443",
  "developer@example.com",
  "-install",
]) {
  test(`invalid extra host fails before TLS configuration or CA trust (${JSON.stringify(host)})`, (t) => {
    const f = fixture(t);
    const { result, calls } = f.runResult({
      GRAM_SSL_EXTRA_HOSTS: `valid.example.com,${host}`,
    });
    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /Invalid GRAM_SSL_EXTRA_HOSTS entry 2/);
    assert.equal(
      calls.some((call) => call.name === "mise" || call.name === "mkcert"),
      false,
    );
  });
}

test("validation accepts wildcard and international DNS names alongside IPv6", (t) => {
  const f = fixture(t);
  const calls = f.run({
    GRAM_SSL_EXTRA_HOSTS: "*.example.com,bücher.example,fd7a:115c:a1e0::1",
  });
  const generated = calls.find(
    (call) => call.name === "mkcert" && call.args[0] === "-cert-file",
  );
  assert.ok(generated);
  assert.deepEqual(generated.args.slice(4), [
    "localhost",
    "host.docker.internal",
    "127.0.0.1",
    "::1",
    "*.example.com",
    "bücher.example",
    "fd7a:115c:a1e0::1",
  ]);
});
