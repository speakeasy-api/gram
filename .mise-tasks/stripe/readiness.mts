// Offline configuration checks plus a loopback health probe. Never returns secrets or IDs.
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { parseTOML } from "confbox";
import { getStripeReadiness, localWebhookTarget } from "./helpers.mts";

export function preflightStripeCLI(): void {
  try {
    execFileSync("stripe", ["version"], { stdio: "pipe", timeout: 10_000 });
  } catch {
    throw new Error(
      "Stripe CLI unavailable. Run `mise install stripe` then rerun setup.",
    );
  }
}

interface Check {
  code: string;
  ok: boolean;
  message: string;
}
export async function getSetupReadiness(
  root = process.cwd(),
  env = process.env,
) {
  const checks: Check[] = [];
  const add = (code: string, ok: boolean, message: string) =>
    checks.push({ code, ok, message });
  try {
    preflightStripeCLI();
    add("cli", true, "Managed Stripe CLI available.");
  } catch {
    add("cli", false, "Install the managed CLI: mise install stripe.");
  }
  let local: Record<string, string> = {};
  try {
    const parsed = parseTOML(
      readFileSync(join(root, "mise.local.toml"), "utf8"),
    ) as { env?: Record<string, unknown> };
    local = Object.fromEntries(
      Object.entries(parsed.env ?? {}).filter(
        (entry): entry is [string, string] => typeof entry[1] === "string",
      ),
    );
  } catch {
    add(
      "saved-config",
      false,
      "Missing or invalid local configuration. Run mise run stripe:setup, then mise run stripe:listen.",
    );
  }
  let targetOK = false;
  try {
    if (env.GRAM_ENVIRONMENT !== "local") throw new Error("not local");
    localWebhookTarget(env.GRAM_SERVER_URL ?? "", env.GRAM_SERVER_PORT ?? "");
    targetOK = true;
  } catch {
    /* fixed diagnostic below */
  }
  add(
    "local-target",
    targetOK,
    targetOK
      ? "Local worktree target validated."
      : "Use GRAM_ENVIRONMENT=local and a loopback server URL matching GRAM_SERVER_PORT.",
  );
  const keyOK = /^(sk|rk)_test_[A-Za-z0-9_]+$/.test(local.STRIPE_API_KEY ?? "");
  const secretOK = /^whsec_[A-Za-z0-9]+$/.test(
    local.STRIPE_WEBHOOK_SECRET ?? "",
  );
  add(
    "credentials",
    keyOK && secretOK,
    keyOK && secretOK
      ? "Test credentials saved. Expiry is not checked offline; CLI-created keys expire (typically 90 days)."
      : "Save a test API key and signing secret with stripe:setup.",
  );
  const same = ["STRIPE_API_KEY", "STRIPE_WEBHOOK_SECRET"].every(
    (name) => !env[name] || env[name] === "unset" || env[name] === local[name],
  );
  add(
    "environment",
    same,
    same
      ? "Current environment agrees with saved configuration; restart server/worker after changes (running process credentials are not inspected)."
      : "Current environment differs from saved configuration. Open a fresh mise shell, reload server/worker and run mise run stripe:listen.",
  );
  const catalog =
    /^price_/.test(local.STRIPE_PRICE_ID_TUM ?? "") &&
    /^mtr_/.test(local.STRIPE_METER_ID_TUM ?? "") &&
    /^bpc_/.test(local.STRIPE_PORTAL_CONFIGURATION_ID ?? "") &&
    local.STRIPE_METER_EVENT_NAME === "tum";
  add(
    "catalog",
    catalog,
    catalog
      ? "Catalog identifiers saved (remote compatibility is validated by setup, not this offline check)."
      : "Catalog configuration incomplete. Run stripe:setup.",
  );
  let serverOK = false;
  const controlPort = env.GRAM_CONTROL_PORT ?? "";
  if (
    targetOK &&
    /^\d+$/.test(controlPort) &&
    Number(controlPort) > 0 &&
    Number(controlPort) <= 65535
  ) {
    try {
      serverOK = (
        await fetch(`http://127.0.0.1:${controlPort}/healthz`, {
          signal: AbortSignal.timeout(2000),
          redirect: "error",
        })
      ).ok;
    } catch {
      /* not ready */
    }
  }
  add(
    "server",
    serverOK,
    serverOK
      ? "Local server health endpoint is ready (not proof it loaded the latest credentials)."
      : "Local server not ready. Run mise run wake; restart server/worker after setup.",
  );
  const listener = getStripeReadiness(root, env);
  add("listener", listener.ready, listener.message);
  return {
    configurationReady: checks.every((check) => check.ok),
    listenerReady: listener.ready,
    serverConfigurationVerified: false,
    deliveryVerified: listener.deliveryVerified,
    checks: [
      ...checks,
      {
        code: "running-configuration",
        ok: false,
        message:
          "Running server/worker configuration is UNKNOWN. Restart server and worker after setup; status does not inspect their credentials. Verify a sandbox webhook and billing transition separately.",
      },
    ],
  };
}
