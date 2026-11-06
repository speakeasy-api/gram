import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import {
  chmodSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { after, describe as suite, test } from "node:test";
import { readEnv, sandboxSettings, validHost } from "./sandbox-host.mts";

const script = resolve(".mise-tasks/flows/sandbox-host.mts");
const miseToml = readFileSync(resolve("mise.toml"), "utf8");
const env = readEnv(miseToml);
const settings = sandboxSettings(env, "host.docker.internal", "0.0.0.0");
const saved = new Map(settings);
const scratch = mkdtempSync(join(tmpdir(), "sandbox-host-test-"));
after(() => rmSync(scratch, { recursive: true, force: true }));

suite("readEnv", () => {
  test("reads the repository's mise.toml without an unreadable host line", () => {
    assert.equal(env["GRAM_HOST"], "localhost");
    assert.equal(env["GRAM_BIND_IP"], "127.0.0.1");
  });

  test("reads only the [env] table", () => {
    const e = readEnv(
      '[tools]\nGRAM_HOST = "nope"\n[env]\nA = "1"\n[settings]\nB = "2"\n',
    );
    assert.deepEqual(e, { A: "1" });
  });

  test("reads plain strings with a trailing comment", () => {
    assert.deepEqual(readEnv('[env]\nA = "x" # why\n'), { A: "x" });
  });

  test("reads an inline default", () => {
    assert.deepEqual(
      readEnv('[env]\nA = { default = "{{env.GRAM_HOST}}" }\n'),
      {
        A: "{{env.GRAM_HOST}}",
      },
    );
  });

  test("keeps escaped quotes", () => {
    assert.deepEqual(readEnv('[env]\nA = "say \\"hi\\""\n'), {
      A: 'say \\"hi\\"',
    });
  });

  test("skips comments, blanks and values it does not need", () => {
    assert.deepEqual(
      readEnv(
        '[env]\n# GRAM_HOST\n\nN = 1\nT = true\nJ = \'{"a": 1}\'\nA = "a"\n',
      ),
      { A: "a" },
    );
  });

  for (const line of [
    "A = 'https://{{env.GRAM_HOST}}'",
    'A = { value = "{{env.GRAM_HOST}}", redact = true }',
    'A = """{{env.GRAM_BIND_IP}}"""',
  ]) {
    test(`fails on a host line it cannot read: ${line}`, () => {
      assert.throws(
        () => readEnv(`[env]\n${line}\n`),
        /Cannot read this mise.toml line/,
      );
    });
  }
});

suite("sandboxSettings", () => {
  test("starts with the host and the bind address", () => {
    assert.deepEqual(settings.slice(0, 2), [
      ["GRAM_HOST", "host.docker.internal"],
      ["GRAM_BIND_IP", "0.0.0.0"],
    ]);
  });

  for (const key of [
    "GRAM_SITE_URL",
    "GRAM_SERVER_URL",
    "GRAM_DEVIDP_EXTERNAL_URL",
    "GRAM_DEVIDP_HOST",
    "GRAM_IDP_BASE_URL",
    "GRAM_SERVER_ADDRESS",
    "GRAM_DEVIDP_ADDRESS",
    "GRAM_APP_URL",
    "GRAM_API_URL",
  ]) {
    test(`re-saves ${key} as its mise.toml template`, () => {
      assert.equal(saved.get(key), env[key]);
      assert.match(saved.get(key)!, /\{\{env\./);
    });
  }

  test("re-saves every template that reads the host, directly or not", () => {
    const keys = new Set(saved.keys());
    for (const [key, value] of Object.entries(env)) {
      for (const ref of value.matchAll(/env\.([A-Za-z_][A-Za-z0-9_]*)/g)) {
        if (
          keys.has(ref[1]!) &&
          !["GRAM_SSL_EXTRA_HOSTS", "VITE_DEV_HOSTNAMES"].includes(key)
        ) {
          assert.ok(
            keys.has(key),
            `${key} reads ${ref[1]} but is not re-saved`,
          );
        }
      }
    }
  });

  test("saves each template after the ones it reads", () => {
    const order = settings.map(([k]) => k);
    for (const [i, [key, value]] of settings.entries()) {
      for (const ref of value.matchAll(/env\.([A-Za-z_][A-Za-z0-9_]*)/g)) {
        const at = order.indexOf(ref[1]!);
        if (at !== -1) assert.ok(at < i, `${key} is saved before ${ref[1]}`);
      }
    }
  });

  test("leaves unrelated settings alone", () => {
    for (const key of [
      "GRAM_SITE_PORT",
      "GRAM_SERVER_PORT",
      "GRAM_DEVIDP_PORT",
      "GRAM_ENVIRONMENT",
    ]) {
      assert.equal(saved.has(key), false, key);
    }
  });

  test("adds the host to the certificate's names", () => {
    assert.equal(saved.get("GRAM_SSL_EXTRA_HOSTS"), "host.docker.internal");
  });

  test("keeps existing certificate names", () => {
    const s = new Map(
      sandboxSettings(
        { ...env, GRAM_SSL_EXTRA_HOSTS: "a.test, b.test" },
        "host.docker.internal",
        "0.0.0.0",
      ),
    );
    assert.equal(
      s.get("GRAM_SSL_EXTRA_HOSTS"),
      "a.test,b.test,host.docker.internal",
    );
  });

  test("adds the host to the dashboard's allowed hosts, keeping localhost", () => {
    assert.equal(
      saved.get("VITE_DEV_HOSTNAMES"),
      "localhost,host.docker.internal",
    );
  });

  test("does not repeat a host already listed", () => {
    const s = new Map(
      sandboxSettings(
        { ...env, VITE_DEV_HOSTNAMES: "localhost,h.test" },
        "h.test",
        "0.0.0.0",
      ),
    );
    assert.equal(s.get("VITE_DEV_HOSTNAMES"), "localhost,h.test");
  });

  test("brackets an IPv6 bind address", () => {
    const s = new Map(sandboxSettings(env, "h.test", "::"));
    assert.equal(s.get("GRAM_BIND_IP"), "[::]");
  });

  for (const ip of [
    "",
    "localhost",
    "0.0.0.0; rm -rf /",
    "256.0.0.1",
    "[::]",
  ]) {
    test(`rejects bind address ${JSON.stringify(ip)}`, () => {
      assert.throws(
        () => sandboxSettings(env, "h.test", ip),
        /Not an IP address/,
      );
    });
  }

  test("never lists the same key twice", () => {
    const keys = settings.map(([k]) => k);
    assert.equal(new Set(keys).size, keys.length);
  });
});

suite("listeners", () => {
  test("the server and dev-idp listen on the bind address", () => {
    assert.equal(saved.get("GRAM_SERVER_ADDRESS"), env["GRAM_SERVER_ADDRESS"]);
    assert.equal(saved.get("GRAM_DEVIDP_ADDRESS"), env["GRAM_DEVIDP_ADDRESS"]);
    assert.match(
      saved.get("GRAM_SERVER_ADDRESS")!,
      /\{\{env\.GRAM_BIND_IP\}\}/,
    );
  });

  test("every other listener stays on loopback", () => {
    const others = [...saved].filter(
      ([key]) =>
        /ADDRESS$/.test(key) &&
        !["GRAM_SERVER_ADDRESS", "GRAM_DEVIDP_ADDRESS"].includes(key),
    );
    assert.ok(others.length >= 2, "expected the admin and mock OIDC listeners");
    for (const [key, value] of others) {
      assert.match(value, /^127\.0\.0\.1:/, key);
      assert.doesNotMatch(value, /GRAM_BIND_IP/, key);
    }
  });

  test("the admin API keeps listening on loopback", () => {
    assert.match(
      saved.get("GRAM_ADMIN_SERVER_ADDRESS")!,
      /^127\.0\.0\.1:\{\{env\.GRAM_ADMIN_PORT\}\}$/,
    );
    assert.match(saved.get("MOCK_OIDC_ADDRESS")!, /^127\.0\.0\.1:/);
  });

  test("keeps a literal listener the developer set", () => {
    const s = new Map(
      sandboxSettings(env, "host.docker.internal", "0.0.0.0", {
        GRAM_SERVER_ADDRESS: "10.0.0.5:9000",
        GRAM_ADMIN_SERVER_ADDRESS: "10.0.0.5:9001",
      }),
    );
    assert.equal(s.get("GRAM_SERVER_ADDRESS"), "10.0.0.5:9000");
    assert.equal(s.get("GRAM_ADMIN_SERVER_ADDRESS"), "10.0.0.5:9001");
  });

  test("replaces a saved template listener", () => {
    const s = new Map(
      sandboxSettings(env, "host.docker.internal", "0.0.0.0", {
        GRAM_SERVER_ADDRESS: "{{env.GRAM_BIND_IP}}:1",
      }),
    );
    assert.equal(s.get("GRAM_SERVER_ADDRESS"), env["GRAM_SERVER_ADDRESS"]);
  });

  test("keeps the developer's certificate names and dashboard hosts", () => {
    const s = new Map(
      sandboxSettings(env, "host.docker.internal", "0.0.0.0", {
        GRAM_SSL_EXTRA_HOSTS: "dev.test",
        VITE_DEV_HOSTNAMES: "localhost,dev.test",
      }),
    );
    assert.equal(
      s.get("GRAM_SSL_EXTRA_HOSTS"),
      "dev.test,host.docker.internal",
    );
    assert.equal(
      s.get("VITE_DEV_HOSTNAMES"),
      "localhost,dev.test,host.docker.internal",
    );
  });
});

suite("validHost", () => {
  for (const host of [
    "host.docker.internal",
    "localhost",
    "devbox",
    "a-b.c-d.test",
    "x1",
  ]) {
    test(`accepts ${host}`, () => assert.ok(validHost(host)));
  }
  for (const host of [
    "",
    "Host.Docker.Internal",
    "-bad.test",
    "bad-.test",
    "a..b",
    ".a",
    "a.",
    "a_b.test",
    "host:5173",
    "https://host",
    "h t",
    "h\nGRAM_BIND_IP=1.2.3.4",
    "a".repeat(64),
    `${"a".repeat(63)}.`.repeat(4) + "a",
  ]) {
    test(`rejects ${JSON.stringify(host).slice(0, 40)}`, () => {
      assert.equal(validHost(host), false);
      assert.throws(
        () => sandboxSettings(env, host, "0.0.0.0"),
        /Not a host name/,
      );
    });
  }
});

suite("the task", () => {
  function runTask(vars: Record<string, string>, localToml?: string) {
    const dir = mkdtempSync(join(scratch, "run-"));
    const bin = join(dir, "bin");
    mkdirSync(bin);
    writeFileSync(join(dir, "mise.toml"), miseToml);
    if (localToml !== undefined) {
      writeFileSync(join(dir, "mise.local.toml"), localToml);
    }
    writeFileSync(
      join(bin, "mise"),
      `#!/usr/bin/env bash\nprintf '%s\\n' "$@" > "${dir}/mise.args"\n`,
    );
    chmodSync(join(bin, "mise"), 0o755);
    const res = spawnSync(process.execPath, [script], {
      cwd: dir,
      encoding: "utf8",
      env: { PATH: `${bin}:${process.env["PATH"]}`, ...vars },
    });
    let args: string[] | undefined;
    try {
      args = readFileSync(join(dir, "mise.args"), "utf8").trim().split("\n");
    } catch {
      args = undefined;
    }
    return { ...res, args };
  }

  test("saves every setting to mise.local.toml in one mise set", () => {
    const r = runTask({
      usage_host: "host.docker.internal",
      usage_bind_ip: "0.0.0.0",
    });
    assert.equal(r.status, 0, r.stderr);
    assert.deepEqual(r.args!.slice(0, 3), ["set", "--file", "mise.local.toml"]);
    assert.deepEqual(
      r.args!.slice(3),
      settings.map(([k, v]) => `${k}=${v}`),
    );
    assert.match(
      r.stdout,
      /serve on host\.docker\.internal, listening on 0\.0\.0\.0/,
    );
  });

  test("a dry run prints and saves nothing", () => {
    const r = runTask({
      usage_host: "host.docker.internal",
      usage_bind_ip: "0.0.0.0",
      usage_dry_run: "true",
    });
    assert.equal(r.status, 0, r.stderr);
    assert.equal(r.args, undefined);
    assert.match(r.stdout, /^GRAM_HOST=host\.docker\.internal$/m);
  });

  test("a bad host fails before anything is saved", () => {
    const r = runTask({
      usage_host: "evil.test\nGRAM_BIND_IP=1.1.1.1",
      usage_bind_ip: "0.0.0.0",
    });
    assert.notEqual(r.status, 0);
    assert.equal(r.args, undefined);
    assert.match(r.stderr, /Not a host name/);
  });

  test("a missing host fails", () => {
    const r = runTask({ usage_bind_ip: "0.0.0.0" });
    assert.notEqual(r.status, 0);
    assert.equal(r.args, undefined);
  });

  test("a missing bind address fails: there is no default", () => {
    const r = runTask({ usage_host: "host.docker.internal" });
    assert.notEqual(r.status, 0);
    assert.equal(r.args, undefined);
    assert.match(r.stderr, /Not an IP address/);
  });

  test("the bind flag is required in the task's usage", () => {
    const source = readFileSync(script, "utf8");
    assert.match(source, /^\/\/USAGE flag "--bind-ip <ip>" required=#true /m);
    assert.doesNotMatch(source, /--bind-ip <ip>"[^\n]*default=/);
  });

  test("keeps literal listeners from mise.local.toml", () => {
    const r = runTask(
      { usage_host: "host.docker.internal", usage_bind_ip: "0.0.0.0" },
      '[env]\nGRAM_SERVER_ADDRESS = "10.0.0.5:9000"\n',
    );
    assert.equal(r.status, 0, r.stderr);
    assert.ok(r.args!.includes("GRAM_SERVER_ADDRESS=10.0.0.5:9000"));
  });
});
