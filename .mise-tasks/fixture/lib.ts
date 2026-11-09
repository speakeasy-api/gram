import { execFileSync } from "node:child_process";
import { randomBytes, X509Certificate } from "node:crypto";
import {
  existsSync,
  lstatSync,
  mkdirSync,
  readFileSync,
  renameSync,
  statSync,
  writeFileSync,
} from "node:fs";
import { createServer } from "node:net";
import { dirname, isAbsolute, join, resolve } from "node:path";
import { createSecureContext } from "node:tls";

export const root = process.env["MISE_PROJECT_ROOT"] ?? process.cwd();
export const envFile = join(root, ".local/mcp-fixture.env");
export const dryRun = process.env["usage_dry_run"] === "true";

export type Config = Record<string, string>;

// Every owned scenario gets a cleanup attempt, including an uncertain PUT.
export async function cleanupScenarios(
  ids: readonly string[],
  remove: (id: string) => Promise<void>,
): Promise<void> {
  const failures: unknown[] = [];
  for (const id of ids) {
    try {
      await remove(id);
    } catch (error) {
      failures.push(error);
    }
  }
  if (failures.length > 0) throw failures[0];
}

function origin(value: string): string {
  const url = new URL(value);
  if (
    url.protocol !== "https:" ||
    url.username ||
    url.password ||
    url.pathname !== "/" ||
    url.search ||
    url.hash
  ) {
    throw new Error(
      "Fixture and client addresses must be HTTPS origins without credentials, paths or query strings.",
    );
  }
  return url.origin;
}

function port(value: string): number {
  if (!/^\d+$/.test(value) || Number(value) < 1024 || Number(value) > 65535) {
    throw new Error("Fixture ports must be integers in 1024–65535.");
  }
  return Number(value);
}

export function readConfig(): Config {
  if (!existsSync(envFile))
    throw new Error("Run GRAM_MCP_FIXTURE=local mise run fixture:env first.");
  const info = lstatSync(envFile);
  if (!info.isFile() || (info.mode & 0o777) !== 0o600) {
    throw new Error(
      "The fixture env file must be a regular private file with mode 0600.",
    );
  }
  const config: Config = {};
  for (const line of readFileSync(envFile, "utf8").trim().split("\n")) {
    const match = /^([A-Z_]+)=(.*)$/.exec(line);
    if (!match) throw new Error("Malformed fixture env file.");
    try {
      const value = JSON.parse(match[2]);
      if (typeof value !== "string") throw new Error();
      config[match[1]] = value;
    } catch {
      throw new Error("Malformed fixture env value; contents withheld.");
    }
  }
  if (config["FIXTURE_CONTROL_SECRET"]?.length !== 64)
    throw new Error("Malformed fixture control secret.");
  for (const key of ["FIXTURE_BASE_URL", "FIXTURE_SMOKE_ORIGIN"]) {
    const url = new URL(origin(config[key]));
    if (url.hostname !== "127.0.0.1")
      throw new Error("The local fixture must bind literal IPv4 loopback.");
    port(url.port);
  }
  return config;
}

async function checkPort(value: number, busyMessage?: string): Promise<void> {
  await new Promise<void>((accept, reject) => {
    const listener = createServer();
    listener.once("error", () =>
      reject(
        new Error(
          busyMessage ??
            `Loopback port ${value} is busy; select an unused worktree port with --port or --client-port.`,
        ),
      ),
    );
    listener.listen({ host: "127.0.0.1", port: value, exclusive: true }, () =>
      listener.close(() => accept()),
    );
  });
}

export function validateTLS(config: Config): void {
  // stat follows a development-key symlink and checks the actual key's mode.
  const keyInfo = statSync(config["FIXTURE_TLS_KEY_FILE"]);
  if (!keyInfo.isFile() || (keyInfo.mode & 0o077) !== 0) {
    throw new Error(
      "The TLS private key target must be a regular file with no group or other permission bits. Supply a private --key-file; fixture tasks do not change existing key permissions.",
    );
  }
  const certificate = readFileSync(config["FIXTURE_TLS_CERT_FILE"]);
  const key = readFileSync(config["FIXTURE_TLS_KEY_FILE"]);
  const ca = readFileSync(config["FIXTURE_CA_FILE"]);
  const leaf = new X509Certificate(certificate);
  const authority = new X509Certificate(ca);
  if (
    !leaf.checkIP("127.0.0.1") ||
    !leaf.verify(authority.publicKey) ||
    Date.parse(leaf.validTo) <= Date.now()
  ) {
    throw new Error(
      "The development certificate must cover 127.0.0.1 and be signed by the configured local CA.",
    );
  }
  createSecureContext({ cert: certificate, key, ca, minVersion: "TLSv1.2" });
}

export async function prepare(): Promise<Config> {
  if (process.env["GRAM_MCP_FIXTURE"] !== "local") {
    throw new Error("Opt in explicitly with GRAM_MCP_FIXTURE=local.");
  }
  const existing = existsSync(envFile) ? readConfig() : undefined;
  const gitPaths = execFileSync(
    "git",
    ["rev-parse", "--git-dir", "--git-common-dir"],
    { cwd: root, encoding: "utf8" },
  )
    .trim()
    .split("\n");
  const linkedWorktree =
    resolve(root, gitPaths[0]) !== resolve(root, gitPaths[1]);
  const allocated: Config = {};
  if (linkedWorktree && !existing && !dryRun) {
    // Read normal worktree allocation without changing any application binding.
    const declarations = execFileSync(
      "mise",
      [
        "run",
        "zero:remap-ports",
        "--preserve",
        "--format",
        "flat",
        "--file",
        "-",
      ],
      { cwd: root, encoding: "utf8", stdio: ["ignore", "pipe", "inherit"] },
    );
    for (const line of declarations.split("\n")) {
      const match = /^(GRAM_MCP_FIXTURE_(?:CLIENT_)?PORT)=(\d+)$/.exec(line);
      if (match) allocated[match[1]] = match[2];
    }
  }
  const listenPort = port(
    process.env["usage_port"] ??
      existing?.["FIXTURE_PORT"] ??
      allocated["GRAM_MCP_FIXTURE_PORT"] ??
      process.env["GRAM_MCP_FIXTURE_PORT"] ??
      "18750",
  );
  const clientPort = port(
    process.env["usage_client_port"] ??
      existing?.["FIXTURE_CLIENT_PORT"] ??
      allocated["GRAM_MCP_FIXTURE_CLIENT_PORT"] ??
      process.env["GRAM_MCP_FIXTURE_CLIENT_PORT"] ??
      "18751",
  );
  if (listenPort === clientPort)
    throw new Error("Fixture and smoke client ports must differ.");
  const baseURL = `https://127.0.0.1:${listenPort}`;
  const smokeOrigin = `https://127.0.0.1:${clientPort}`;
  let ca =
    process.env["usage_ca_file"] ??
    existing?.["FIXTURE_CA_FILE"] ??
    process.env["NODE_EXTRA_CA_CERTS"];
  if (!ca && !dryRun)
    ca = join(
      execFileSync("mkcert", ["-CAROOT"], { encoding: "utf8" }).trim(),
      "rootCA.pem",
    );
  const config: Config = {
    FIXTURE_PORT: String(listenPort),
    FIXTURE_CLIENT_PORT: String(clientPort),
    FIXTURE_BASE_URL: baseURL,
    FIXTURE_SMOKE_ORIGIN: smokeOrigin,
    FIXTURE_CONTROL_SECRET:
      existing?.["FIXTURE_CONTROL_SECRET"] ??
      (dryRun ? "<generated privately>" : randomBytes(32).toString("hex")),
    FIXTURE_TLS_CERT_FILE: resolve(
      process.env["usage_cert_file"] ??
        existing?.["FIXTURE_TLS_CERT_FILE"] ??
        process.env["GRAM_SSL_CERT_FILE"] ??
        join(root, "local/ssl/certs/local.pem"),
    ),
    FIXTURE_TLS_KEY_FILE: resolve(
      process.env["usage_key_file"] ??
        existing?.["FIXTURE_TLS_KEY_FILE"] ??
        process.env["GRAM_SSL_KEY_FILE"] ??
        join(root, "local/ssl/keys/local-key.pem"),
    ),
    FIXTURE_CA_FILE: ca ? resolve(ca) : "<mkcert CA>/rootCA.pem",
    FIXTURE_CLIENT_ORIGINS: [
      ...new Set(
        [
          process.env["GRAM_SERVER_URL"],
          process.env["GRAM_SITE_URL"],
          smokeOrigin,
        ]
          .filter(Boolean)
          .map((value) => origin(value!)),
      ),
    ].join(","),
    FIXTURE_BIN:
      process.env["usage_bin"] ??
      existing?.["FIXTURE_BIN"] ??
      "benchmark-mcp-server",
  };
  if (
    config["FIXTURE_BIN"] !== "benchmark-mcp-server" &&
    !isAbsolute(config["FIXTURE_BIN"])
  ) {
    throw new Error("--bin must name an absolute executable path.");
  }
  if (dryRun) {
    console.log(
      `Would prepare ${envFile} (0600), bind ${baseURL}, and allow the worktree server/dashboard plus the isolated HTTPS smoke client.`,
    );
    console.log(
      "Would validate the development certificate/CA and both loopback ports; no secret, file or daemon is created.",
    );
    if (linkedWorktree && !existing)
      console.log(
        "Would allocate missing fixture ports through worktree remapping and reserve only those two bindings in mise.local.toml.",
      );
    return config;
  }
  validateTLS(config);
  const changed =
    existing && JSON.stringify(existing) !== JSON.stringify(config);
  if (changed)
    await checkPort(
      port(existing["FIXTURE_PORT"]),
      "Stop this worktree's fixture with mise run fixture:stop before changing its configuration. If it is already stopped, free its stored port first.",
    );
  if (!existing || changed) {
    await checkPort(listenPort);
    await checkPort(clientPort);
    mkdirSync(dirname(envFile), { recursive: true, mode: 0o700 });
    const temporary = `${envFile}.tmp-${process.pid}`;
    const text =
      Object.entries(config)
        .map(([key, value]) => `${key}=${JSON.stringify(value)}`)
        .join("\n") + "\n";
    writeFileSync(temporary, text, { mode: 0o600, flag: "wx" });
    renameSync(temporary, envFile);
  }
  if (linkedWorktree) {
    // Reservations are visible to sibling worktrees even while this fixture is stopped.
    execFileSync(
      "mise",
      [
        "set",
        "--file",
        join(root, "mise.local.toml"),
        `GRAM_MCP_FIXTURE_PORT=${listenPort}`,
        `GRAM_MCP_FIXTURE_CLIENT_PORT=${clientPort}`,
      ],
      { cwd: root, stdio: "ignore" },
    );
  }
  return config;
}

export function fail(error: unknown): never {
  console.error(
    error instanceof Error ? error.message : "Fixture task failed.",
  );
  process.exit(1);
}
