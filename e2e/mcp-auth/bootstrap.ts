import { chromium, expect, type APIResponse } from "@playwright/test";
import { execFileSync, spawnSync } from "node:child_process";
import {
  chmodSync,
  existsSync,
  lstatSync,
  mkdirSync,
  readFileSync,
  realpathSync,
  writeFileSync,
} from "node:fs";
import { dirname, isAbsolute, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const credentialName = "auth-e2e-platform-ema-signer";
const serviceAccount = "auth-e2e-signer@local-signing.iam.gserviceaccount.com";
const keys = [
  "GRAM_IDENTITY_PROVIDER_SIGNING_CREDENTIAL_ID",
  "GRAM_LOCAL_FEATURE_FLAGS_CSV",
  "GRAM_IDENTITY_PROVIDER_SIGNING_SERVICE_ACCOUNT",
] as const;
// The Speakeasy issuer can vouch for a sign-in only with both flags on.
const scopedFlags = ["internal-session-issuer", "sign-in-enforcement"] as const;
let stage = "prerequisites";
class BootstrapError extends Error {}
function requireSafe(ok: unknown, code: string): asserts ok {
  if (!ok) throw new BootstrapError(code);
}
export function loopbackURL(value: string | undefined): string {
  requireSafe(value, "MISSING_LOCAL_URL");
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    throw new BootstrapError("INVALID_LOCAL_URL");
  }
  requireSafe(
    ["localhost", "127.0.0.1", "[::1]"].includes(url.hostname) &&
      ["http:", "https:"].includes(url.protocol) &&
      !url.username &&
      !url.password &&
      !url.search &&
      !url.hash,
    "NONLOCAL_OR_CREDENTIAL_URL",
  );
  return url.origin;
}
export function scopedCSV(source: string, organization: string): string {
  requireSafe(
    /^[A-Za-z0-9_-]+$/.test(organization) && organization !== "*",
    "INVALID_ORGANIZATION",
  );
  requireSafe(source.trim().length > 0, "EMPTY_FLAGS_CSV");
  const lines = source.split(/\r?\n/);
  const missing = scopedFlags
    .map((flag) => `${organization},${flag},true`)
    .filter((row) => {
      const key = row.slice(0, row.lastIndexOf(",") + 1);
      return [...lines].reverse().find((line) => line.startsWith(key)) !== row;
    });
  if (missing.length === 0) return source;
  return (
    source + (source.endsWith("\n") ? "" : "\n") + missing.join("\n") + "\n"
  );
}
export function originalFlagsPath(
  configured: string | undefined,
  output: string,
  manifest?: { previousEffective?: Record<string, string | null> },
): string | undefined {
  if (configured !== output) return configured;
  const previous = manifest?.previousEffective;
  requireSafe(
    previous && "GRAM_LOCAL_FEATURE_FLAGS_CSV" in previous,
    "ORIGINAL_FLAGS_SNAPSHOT_REQUIRED",
  );
  const source = previous.GRAM_LOCAL_FEATURE_FLAGS_CSV ?? undefined;
  requireSafe(source !== output, "RECURSIVE_FLAGS_SNAPSHOT");
  return source;
}
function contained(root: string, path: string) {
  const rel = relative(root, path);
  requireSafe(
    rel !== ".." && !rel.startsWith("../") && !isAbsolute(rel),
    "PATH_OUTSIDE_WORKTREE",
  );
  let current = path;
  while (current !== root) {
    if (existsSync(current))
      requireSafe(!lstatSync(current).isSymbolicLink(), "SYMLINK_REFUSED");
    current = dirname(current);
  }
}
async function responseJSON(response: APIResponse, operation: string) {
  if (!response.ok()) {
    // Never serialize API bodies: they can contain user details and session material.
    let code = "unknown";
    try {
      const body = await response.json();
      const candidate = body.name ?? body.code;
      if (
        [
          "unauthorized",
          "forbidden",
          "bad_request",
          "not_found",
          "internal_error",
        ].includes(candidate)
      )
        code = candidate;
    } catch {
      /* Status remains sufficient if the response is not JSON. */
    }
    throw new BootstrapError(`${operation}_HTTP_${response.status()}_${code}`);
  }
  return response.json();
}

async function main() {
  requireSafe(
    process.env.GRAM_ENVIRONMENT === "local",
    "LOCAL_ENVIRONMENT_REQUIRED",
  );
  const root = realpathSync(fileURLToPath(new URL("../..", import.meta.url)));
  requireSafe(
    realpathSync(process.cwd()) === root &&
      process.env.MCP_AUTH_CONFIG_ROOT === root &&
      process.env.MCP_AUTH_SERVER_CWD === join(root, "server"),
    "CONFIG_ROOT_OR_SERVER_CWD_MISMATCH",
  );
  contained(root, join(root, "server"));
  const site = loopbackURL(process.env.GRAM_SITE_URL);
  const server = loopbackURL(process.env.GRAM_SERVER_URL);
  for (const [key, value] of Object.entries(process.env)) {
    if (value && /(?:SITE|SERVER|APP|API|ADMIN)_URL$/.test(key))
      loopbackURL(value);
  }
  const configuredAccount =
    process.env.GRAM_IDENTITY_PROVIDER_SIGNING_SERVICE_ACCOUNT;
  requireSafe(
    !configuredAccount || configuredAccount === serviceAccount,
    "UNRELATED_SIGNING_SERVICE_ACCOUNT",
  );
  const outputPath = join(root, "server/.mcp-auth/flags.local.csv");
  const manifestPath = join(root, ".logs/mcp-auth/bootstrap/manifest.json");
  const configuredSource = process.env.GRAM_LOCAL_FEATURE_FLAGS_CSV;
  const sourcePath = originalFlagsPath(
    configuredSource,
    outputPath,
    existsSync(manifestPath)
      ? JSON.parse(readFileSync(manifestPath, "utf8"))
      : undefined,
  );
  if (sourcePath) {
    requireSafe(isAbsolute(sourcePath), "ABSOLUTE_FLAGS_CSV_REQUIRED");
    contained(join(root, "server"), sourcePath);
    if (existsSync(sourcePath))
      requireSafe(lstatSync(sourcePath).isFile(), "FLAGS_CSV_NOT_REGULAR_FILE");
  }
  // Rebuild from the original input, never from a previous organization's rollout.
  const source =
    sourcePath && existsSync(sourcePath)
      ? readFileSync(sourcePath, "utf8")
      : "distinct_id,flag,enabled\n";
  const config = join(root, "mise.local.toml");
  for (const path of [outputPath, manifestPath, config]) contained(root, path);
  for (const path of [outputPath, manifestPath]) {
    execFileSync("git", ["check-ignore", "--quiet", path], {
      cwd: root,
      stdio: "pipe",
    });
  }
  // Read only the selected settings, not the complete local configuration.
  const previous = Object.fromEntries(
    keys.map((key) => {
      const result = spawnSync(
        "mise",
        ["config", "get", "-f", config, `env.${key}`],
        { cwd: root, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] },
      );
      requireSafe(
        result.status === 0 ||
          (result.status === 1 &&
            result.stderr.includes(`Key not found: env.${key} `)),
        "SELECTED_CONFIG_READ_FAILED",
      );
      return [
        key,
        result.status === 0
          ? { present: true, storedValue: result.stdout.trimEnd() }
          : { present: false },
      ];
    }),
  );
  stage = "chrome_launch";
  const browser = await chromium.launch({
    channel: "chrome",
    chromiumSandbox: true,
  });
  try {
    const context = await browser.newContext({ ignoreHTTPSErrors: true });
    context.setDefaultTimeout(30_000);
    context.setDefaultNavigationTimeout(60_000);
    const page = await context.newPage();
    // A new in-memory context must complete the real dashboard login, never storage injection.
    stage = "dashboard_login";
    await page.goto(site);
    const login = page.getByRole("button", { name: "Log in", exact: true });
    await expect(login).toBeVisible({ timeout: 60_000 });
    await login.click();
    const add = page.getByRole("button", { name: "Add New", exact: true });
    const home = page.getByRole("link", { name: "Home", exact: true });
    await expect(add.or(home).first()).toBeVisible({ timeout: 60_000 });
    if (!(await add.isVisible())) await home.click();
    await expect(add).toBeVisible();
    requireSafe(
      loopbackURL(page.url()) === site,
      "UNEXPECTED_DASHBOARD_ORIGIN",
    );
    const slug = new URL(page.url()).pathname.split("/").filter(Boolean)[0];
    stage = "session_info";
    const session = await responseJSON(
      await context.request.get(`${server}/rpc/auth.info`, {
        maxRedirects: 0,
        maxRetries: 0,
      }),
      "AUTH_INFO",
    );
    requireSafe(
      session.is_admin === true &&
        session.organization_override === false &&
        !session.impersonator_email,
      "ORDINARY_PLATFORM_ADMIN_SESSION_REQUIRED",
    );
    const organization = session.active_organization_id;
    requireSafe(
      Array.isArray(session.organizations) &&
        session.organizations.some(
          (entry: { id: string; slug: string }) =>
            entry.id === organization && entry.slug === slug,
        ),
      "ACTIVE_MEMBER_ORGANIZATION_REQUIRED",
    );
    const csv = scopedCSV(source, organization);
    stage = "platform_credential_list";
    const listing = await responseJSON(
      await context.request.get(
        `${server}/rpc/adminExternalCredentials.list?provider=gcp_iam`,
        { maxRedirects: 0, maxRetries: 0 },
      ),
      "PLATFORM_CREDENTIAL_LIST",
    );
    requireSafe(Array.isArray(listing.credentials), "INVALID_CREDENTIAL_LIST");
    const matches = listing.credentials.filter(
      (entry: { name: string }) => entry.name === credentialName,
    );
    requireSafe(matches.length <= 1, "DUPLICATE_OWNED_CREDENTIAL");
    const configured = process.env.GRAM_IDENTITY_PROVIDER_SIGNING_CREDENTIAL_ID;
    requireSafe(
      !configured || (matches.length === 1 && matches[0].id === configured),
      "UNRELATED_CONFIGURED_CREDENTIAL",
    );
    const created = matches.length === 0;
    stage = "platform_credential_selection";
    const credential = created
      ? await responseJSON(
          await context.request.post(
            `${server}/rpc/adminExternalCredentials.createGcpIam`,
            {
              data: {
                name: credentialName,
                impersonate_service_account: serviceAccount,
              },
              maxRedirects: 0,
              maxRetries: 0,
            },
          ),
          "PLATFORM_CREDENTIAL_CREATE",
        )
      : await responseJSON(
          await context.request.get(
            `${server}/rpc/adminExternalCredentials.getGcpIam?id=${encodeURIComponent(matches[0].id)}`,
            { maxRedirects: 0, maxRetries: 0 },
          ),
          "PLATFORM_CREDENTIAL_GET",
        );
    requireSafe(
      credential.name === credentialName &&
        credential.provider === "gcp_iam" &&
        !credential.organization_id &&
        !credential.project_id &&
        /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(
          credential.id,
        ) &&
        credential.impersonate_service_account === serviceAccount &&
        !credential.wif_pool_id &&
        !credential.wif_provider_id,
      "OWNED_PLATFORM_CREDENTIAL_VALIDATION_FAILED",
    );
    stage = "private_config_write";
    mkdirSync(dirname(manifestPath), { recursive: true, mode: 0o700 });
    if (!existsSync(manifestPath))
      writeFileSync(
        manifestPath,
        JSON.stringify(
          {
            version: 1,
            previous,
            previousEffective: Object.fromEntries(
              keys.map((key) => [key, process.env[key] ?? null]),
            ),
            selectedCredential: credential.id,
            created,
            privateCSV: outputPath,
          },
          null,
          2,
        ) + "\n",
        { mode: 0o600, flag: "wx" },
      );
    else {
      const manifest = JSON.parse(readFileSync(manifestPath, "utf8"));
      requireSafe(
        manifest.selectedCredential === credential.id &&
          manifest.privateCSV === outputPath,
        "MANIFEST_CONFLICT",
      );
      // Preserve prior snapshots when a new deployment setting is introduced.
      for (const key of keys) {
        if (!(key in manifest.previous)) {
          manifest.previous[key] = previous[key];
          manifest.previousEffective[key] = process.env[key] ?? null;
        }
      }
      writeFileSync(manifestPath, JSON.stringify(manifest, null, 2) + "\n", {
        mode: 0o600,
      });
    }
    chmodSync(manifestPath, 0o600);
    mkdirSync(dirname(outputPath), { recursive: true, mode: 0o700 });
    if (existsSync(outputPath)) chmodSync(outputPath, 0o600);
    writeFileSync(outputPath, csv, { mode: 0o600 });
    chmodSync(outputPath, 0o600);
    execFileSync(
      "mise",
      [
        "set",
        "--file",
        config,
        `${keys[0]}=${credential.id}`,
        `${keys[1]}=${outputPath}`,
        `${keys[2]}=${serviceAccount}`,
      ],
      { cwd: root, stdio: "pipe" },
    );
    console.log(
      `SETUP_READY credential=${created ? "created" : "reused"} scoped_flag=enabled runtime_keys=3 browser=released_after_exit`,
    );
    console.log(
      "Restart required (not executed): mise exec -- pitchfork restart server",
    );
  } finally {
    await browser.close();
  }
}
if (
  process.argv[1] &&
  resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  main().catch((error) => {
    console.error(
      `BOOTSTRAP_STOPPED ${error instanceof BootstrapError ? error.message : `LOCAL_BOOTSTRAP_FAILED_stage_${stage}`}`,
    );
    process.exitCode = 1;
  });
}
