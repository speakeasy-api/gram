#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Provision and validate Stripe sandbox billing objects and initialize the local webhook secret"
//USAGE flag "--listen" help="Opt this worktree into Pitchfork-managed webhook forwarding"

// Idempotent: the meter is keyed on its event name and the price on its
// lookup key, so re-running against a sandbox that already has the objects
// just re-saves the existing IDs. Safe to run any time; refuses live-mode
// keys outright.

import { intro, isCancel, log, note, outro, password } from "@clack/prompts";
import { $ } from "zx";
import { execFileSync } from "node:child_process";
import { lstatSync } from "node:fs";
import { join } from "node:path";
import { localWebhookTarget, registerStripeListener } from "./helpers.mts";
import { getSetupReadiness, preflightStripeCLI } from "./readiness.mts";

const METER_EVENT_NAME = "tum";
const METER_DISPLAY_NAME = "Tokens under management";
const PRICE_LOOKUP_KEY = "payg-tum";
const PRODUCT_NAME = "AI Control Plane PAYG";
const PORTAL_CONFIGURATION_PURPOSE = "gram-payg";
const PRODUCT_METADATA_SPEAKEASY_PRODUCT = "aicp";
// $0.35 per 1M TUMs, linear per-unit, expressed in cents per TUM.
const UNIT_AMOUNT_DECIMAL_CENTS = "0.000035";

interface StripeError {
  error?: { message?: string; type?: string };
}

interface StripeAccount {
  id: string;
  livemode: boolean;
  settings?: { dashboard?: { display_name?: string } };
}

interface StripeMeter {
  id: string;
  event_name: string;
  status: string;
  livemode: boolean;
  default_aggregation?: { formula?: string };
  value_settings?: { event_payload_key?: string };
  customer_mapping?: { type?: string; event_payload_key?: string };
}

interface StripeProduct {
  active: boolean;
  id: string;
  name: string;
  metadata?: Record<string, string>;
}

interface StripePrice {
  id: string;
  product: string;
  active: boolean;
  livemode: boolean;
  currency: string;
  billing_scheme: string;
  unit_amount_decimal?: string;
  recurring?: {
    interval?: string;
    interval_count?: number;
    usage_type?: string;
    meter?: string;
  };
}

interface StripePortalConfiguration {
  id: string;
  active: boolean;
  livemode: boolean;
  metadata?: Record<string, string>;
  features?: {
    customer_update?: { enabled?: boolean };
    invoice_history?: { enabled?: boolean };
    payment_method_update?: { enabled?: boolean };
    subscription_cancel?: {
      enabled?: boolean;
      mode?: string;
      proration_behavior?: string;
      cancellation_reason?: { enabled?: boolean };
    };
    subscription_update?: { enabled?: boolean };
  };
}

interface StripeList<T> {
  data: T[];
  has_more: boolean;
}

function assertConfiguration(
  objectName: string,
  checks: Array<[field: string, actual: unknown, expected: unknown]>,
  remediation = "Archive the object in the Stripe sandbox and re-run this task.",
) {
  const mismatches = checks.filter(
    ([, actual, expected]) => actual !== expected,
  );
  if (mismatches.length === 0) return;

  const details = mismatches
    .map(
      ([field, actual, expected]) =>
        `${field} is ${JSON.stringify(actual)} (expected ${JSON.stringify(expected)})`,
    )
    .join("; ");
  throw new Error(
    `${objectName} exists but is misconfigured: ${details}. ` + remediation,
  );
}

async function stripe<T>(
  key: string,
  method: "GET" | "POST",
  path: string,
  params?: Record<string, string>,
): Promise<T> {
  let url = `https://api.stripe.com/v1${path}`;
  const init: RequestInit = {
    method,
    headers: {
      Authorization: `Bearer ${key}`,
    },
  };
  if (params && method === "GET") {
    url += `?${new URLSearchParams(params)}`;
  } else if (params) {
    init.headers = {
      ...init.headers,
      "Content-Type": "application/x-www-form-urlencoded",
    };
    init.body = new URLSearchParams(params).toString();
  }

  const res = await fetch(url, init);
  const body = (await res.json()) as T & StripeError;
  if (!res.ok) {
    throw new Error(
      `Stripe ${method} ${path} failed (${res.status}): Request rejected; inspect the sandbox request log (do not share secrets or tenant details)`,
    );
  }
  return body;
}

async function findPortalConfigurations(
  key: string,
): Promise<StripePortalConfiguration[]> {
  const matches: StripePortalConfiguration[] = [];
  let startingAfter: string | undefined;

  do {
    const params: Record<string, string> = { active: "true", limit: "100" };
    if (startingAfter) params.starting_after = startingAfter;

    const configurations = await stripe<StripeList<StripePortalConfiguration>>(
      key,
      "GET",
      "/billing_portal/configurations",
      params,
    );
    matches.push(
      ...configurations.data.filter(
        (candidate) =>
          candidate.metadata?.purpose === PORTAL_CONFIGURATION_PURPOSE,
      ),
    );
    if (!configurations.has_more) return matches;

    startingAfter = configurations.data.at(-1)?.id;
    if (!startingAfter) {
      throw new Error(
        "Stripe returned an invalid paginated portal configuration list.",
      );
    }
  } while (true);
}

function assertPortalConfiguration(configuration: StripePortalConfiguration) {
  assertConfiguration(
    `Billing Portal configuration ${configuration.id}`,
    [
      ["active", configuration.active, true],
      ["livemode", configuration.livemode, false],
      [
        "metadata.purpose",
        configuration.metadata?.purpose,
        PORTAL_CONFIGURATION_PURPOSE,
      ],
      [
        "features.customer_update.enabled",
        configuration.features?.customer_update?.enabled,
        false,
      ],
      [
        "features.invoice_history.enabled",
        configuration.features?.invoice_history?.enabled,
        true,
      ],
      [
        "features.payment_method_update.enabled",
        configuration.features?.payment_method_update?.enabled,
        true,
      ],
      [
        "features.subscription_cancel.enabled",
        configuration.features?.subscription_cancel?.enabled,
        true,
      ],
      [
        "features.subscription_cancel.mode",
        configuration.features?.subscription_cancel?.mode,
        "at_period_end",
      ],
      [
        "features.subscription_cancel.proration_behavior",
        configuration.features?.subscription_cancel?.proration_behavior,
        "none",
      ],
      [
        "features.subscription_cancel.cancellation_reason.enabled",
        configuration.features?.subscription_cancel?.cancellation_reason
          ?.enabled,
        false,
      ],
      [
        "features.subscription_update.enabled",
        configuration.features?.subscription_update?.enabled,
        false,
      ],
    ],
    "Deactivate the tagged configuration in the Stripe sandbox and re-run this task.",
  );
}

async function resolveSecretKey(): Promise<{ key: string; prompted: boolean }> {
  const fromEnv = process.env.STRIPE_API_KEY;
  if (fromEnv && fromEnv !== "unset") {
    return { key: fromEnv, prompted: false };
  }

  // An authenticated Stripe CLI carries a short-lived test-mode key. Use it
  // for provisioning, the server and listener; persist it consistently. It
  // expires (typically after 90 days), so this is not durable readiness.
  const cliConfig = await $({
    nothrow: true,
    quiet: true,
  })`stripe config --list`;
  const cliKey = cliConfig.stdout.match(
    /test_mode_api_key\s*=\s*['"]?((?:sk|rk)_test_[A-Za-z0-9_]+)/,
  )?.[1];
  if (cliKey) {
    log.info(
      "Using the CLI test key for all local Stripe clients. CLI keys expire (typically after 90 days). After expiry, replace the saved key with `mise set --prompt --file mise.local.toml STRIPE_API_KEY` and rerun setup. CLI reauthentication alone does not replace the saved key.",
    );
    return { key: cliKey, prompted: false };
  }

  if (!process.stdin.isTTY) {
    log.error(
      "STRIPE_API_KEY is not set and there is no terminal to prompt on. " +
        "Set it securely (`mise set --prompt --file mise.local.toml STRIPE_API_KEY`) and re-run.",
    );
    process.exit(1);
  }

  const entered = await password({
    message:
      "Paste your Stripe sandbox secret key (Developers → API keys in the sandbox):",
    validate: (v) => {
      if (!v) return "A key is required.";
      if (!/^(sk|rk)_test_/.test(v)) {
        return "Only sandbox/test-mode keys (sk_test_… / rk_test_…) are accepted.";
      }
      return undefined;
    },
  });
  if (isCancel(entered)) {
    outro("Cancelled.");
    process.exit(1);
  }
  return { key: entered, prompted: true };
}

async function resolveWebhookSecret(key: string): Promise<string> {
  const listener = await $({
    nothrow: true,
    quiet: true,
    env: { ...process.env, STRIPE_API_KEY: key },
  })`stripe listen --print-secret --skip-update`;
  const output = `${listener.stdout}\n${listener.stderr}`;
  const secret = output.match(/whsec_[A-Za-z0-9]+/)?.[0];
  if (listener.exitCode !== 0 || !secret) {
    throw new Error(
      "Stripe CLI webhook authentication failed. Refresh the sandbox key or CLI login and rerun setup; do not share raw CLI output.",
    );
  }
  return secret;
}

async function findMeter(
  key: string,
  status: "active" | "inactive",
): Promise<StripeMeter | undefined> {
  let startingAfter: string | undefined;
  do {
    const params: Record<string, string> = { status, limit: "100" };
    if (startingAfter) params.starting_after = startingAfter;

    const meters = await stripe<StripeList<StripeMeter>>(
      key,
      "GET",
      "/billing/meters",
      params,
    );
    const meter = meters.data.find(
      (candidate) => candidate.event_name === METER_EVENT_NAME,
    );
    if (meter || !meters.has_more) return meter;

    startingAfter = meters.data.at(-1)?.id;
    if (!startingAfter) {
      throw new Error("Stripe returned an invalid paginated meter list.");
    }
  } while (true);
}

function assertMeterConfiguration(
  meter: StripeMeter,
  expectedStatus: "active" | "inactive",
) {
  assertConfiguration(
    `Meter ${meter.id}`,
    [
      ["status", meter.status, expectedStatus],
      ["livemode", meter.livemode, false],
      [
        "default_aggregation.formula",
        meter.default_aggregation?.formula,
        "sum",
      ],
      [
        "value_settings.event_payload_key",
        meter.value_settings?.event_payload_key,
        "value",
      ],
      ["customer_mapping.type", meter.customer_mapping?.type, "by_id"],
      [
        "customer_mapping.event_payload_key",
        meter.customer_mapping?.event_payload_key,
        "stripe_customer_id",
      ],
    ],
    "Use a fresh Stripe sandbox or coordinate a new meter event name before re-running.",
  );
}

export async function provisionCatalog(key: string) {
  if (!/^(sk|rk)_test_[A-Za-z0-9_]+$/.test(key)) {
    throw new Error("Only Stripe sandbox/test-mode keys are accepted.");
  }
  const account = await stripe<StripeAccount>(key, "GET", "/account");
  if (account.livemode === true) {
    throw new Error("Refusing to run against a live-mode Stripe account.");
  }
  log.info("Connected to Stripe sandbox account.");

  // Meter — event names are unique across active and inactive meters.
  let meter = await findMeter(key, "active");
  if (meter) {
    log.info(`Meter "${METER_EVENT_NAME}" already exists: ${meter.id}`);
  } else {
    meter = await findMeter(key, "inactive");
    if (meter) {
      assertMeterConfiguration(meter, "inactive");
      meter = await stripe<StripeMeter>(
        key,
        "POST",
        `/billing/meters/${meter.id}/reactivate`,
      );
      log.success(`Reactivated meter "${METER_EVENT_NAME}": ${meter.id}`);
    } else {
      meter = await stripe<StripeMeter>(key, "POST", "/billing/meters", {
        display_name: METER_DISPLAY_NAME,
        event_name: METER_EVENT_NAME,
        "default_aggregation[formula]": "sum",
        "value_settings[event_payload_key]": "value",
        "customer_mapping[type]": "by_id",
        "customer_mapping[event_payload_key]": "stripe_customer_id",
      });
      log.success(`Created meter "${METER_EVENT_NAME}": ${meter.id}`);
    }
  }
  assertMeterConfiguration(meter, "active");

  // Price and product — keyed on the lookup key. Creating them in one request
  // avoids leaving an orphan product if price creation fails.
  const prices = await stripe<StripeList<StripePrice>>(key, "GET", "/prices", {
    "lookup_keys[]": PRICE_LOOKUP_KEY,
    active: "true",
    limit: "1",
  });
  let price = prices.data?.[0];
  if (price) {
    log.info(`Price "${PRICE_LOOKUP_KEY}" already exists: ${price.id}`);
  } else {
    const inactivePrices = await stripe<StripeList<StripePrice>>(
      key,
      "GET",
      "/prices",
      {
        "lookup_keys[]": PRICE_LOOKUP_KEY,
        active: "false",
        limit: "1",
      },
    );
    const createParams: Record<string, string> = {
      "product_data[name]": PRODUCT_NAME,
      "product_data[metadata][speakeasy_product]":
        PRODUCT_METADATA_SPEAKEASY_PRODUCT,
      lookup_key: PRICE_LOOKUP_KEY,
      nickname: "PAYG TUM ($0.35 per 1M)",
      currency: "usd",
      billing_scheme: "per_unit",
      unit_amount_decimal: UNIT_AMOUNT_DECIMAL_CENTS,
      "recurring[interval]": "month",
      "recurring[usage_type]": "metered",
      "recurring[meter]": meter.id,
    };
    if (inactivePrices.data?.[0]) {
      createParams.transfer_lookup_key = "true";
    }
    price = await stripe<StripePrice>(key, "POST", "/prices", createParams);
    log.success(`Created price "${PRICE_LOOKUP_KEY}": ${price.id}`);
  }
  assertConfiguration(`Price ${price.id}`, [
    ["active", price.active, true],
    ["livemode", price.livemode, false],
    ["currency", price.currency, "usd"],
    ["billing_scheme", price.billing_scheme, "per_unit"],
    [
      "unit_amount_decimal",
      price.unit_amount_decimal,
      UNIT_AMOUNT_DECIMAL_CENTS,
    ],
    ["recurring.interval", price.recurring?.interval, "month"],
    ["recurring.interval_count", price.recurring?.interval_count, 1],
    ["recurring.usage_type", price.recurring?.usage_type, "metered"],
    ["recurring.meter", price.recurring?.meter, meter.id],
  ]);

  // Only the PAYG product may be tagged; a conflicting tag is fixed by hand.
  const product = await stripe<StripeProduct>(
    key,
    "GET",
    `/products/${price.product}`,
  );
  // The lookup key and validated billing semantics identify the catalog.
  // Display names are mutable; never rename an existing compatible product.
  assertConfiguration(
    "PAYG product",
    [["active", product.active, true]],
    "Use an active sandbox product/price; setup will not reactivate archived products.",
  );
  if (product.name !== PRODUCT_NAME)
    log.warn(
      "Reusing a compatible PAYG product with a different display name; leaving its name unchanged.",
    );
  const productTag = product.metadata?.speakeasy_product;
  if (productTag && productTag !== PRODUCT_METADATA_SPEAKEASY_PRODUCT) {
    throw new Error(
      `Product ${product.id} has unexpected metadata.speakeasy_product=${productTag}; fix the product association before re-running.`,
    );
  }
  if (!productTag) {
    await stripe<StripeProduct>(key, "POST", `/products/${product.id}`, {
      "metadata[speakeasy_product]": PRODUCT_METADATA_SPEAKEASY_PRODUCT,
    });
    log.success(
      `Tagged product ${product.id} with metadata.speakeasy_product=${PRODUCT_METADATA_SPEAKEASY_PRODUCT}`,
    );
  } else {
    log.info(
      `Product ${product.id} already tagged speakeasy_product=${PRODUCT_METADATA_SPEAKEASY_PRODUCT}`,
    );
  }

  // Billing Portal — use a dedicated, tagged configuration so production
  // behavior cannot drift when someone edits Stripe's mutable default.
  const portalConfigurations = await findPortalConfigurations(key);
  if (portalConfigurations.length > 1) {
    throw new Error(
      `Found multiple active Billing Portal configurations tagged metadata.purpose=${PORTAL_CONFIGURATION_PURPOSE}: ` +
        `${portalConfigurations.map((candidate) => candidate.id).join(", ")}. ` +
        "Deactivate all but one in the Stripe sandbox and re-run this task.",
    );
  }
  let portalConfiguration = portalConfigurations[0];
  if (portalConfiguration) {
    log.info(
      `Billing Portal configuration "${PORTAL_CONFIGURATION_PURPOSE}" already exists: ${portalConfiguration.id}`,
    );
  } else {
    portalConfiguration = await stripe<StripePortalConfiguration>(
      key,
      "POST",
      "/billing_portal/configurations",
      {
        "metadata[purpose]": PORTAL_CONFIGURATION_PURPOSE,
        "features[customer_update][enabled]": "false",
        "features[invoice_history][enabled]": "true",
        "features[payment_method_update][enabled]": "true",
        "features[subscription_cancel][enabled]": "true",
        "features[subscription_cancel][mode]": "at_period_end",
        "features[subscription_cancel][proration_behavior]": "none",
        "features[subscription_cancel][cancellation_reason][enabled]": "false",
        "features[subscription_cancel][cancellation_reason][options][0]":
          "too_expensive",
        "features[subscription_cancel][cancellation_reason][options][1]":
          "other",
        "features[subscription_update][enabled]": "false",
      },
    );
    log.success(
      `Created Billing Portal configuration "${PORTAL_CONFIGURATION_PURPOSE}": ${portalConfiguration.id}`,
    );
  }
  assertPortalConfiguration(portalConfiguration);

  return { meter, price, portalConfiguration };
}

interface SetupDependencies {
  preflight: () => Promise<void>;
  resolveKey: () => Promise<{ key: string }>;
  webhookSecret: (key: string) => Promise<string>;
  provision: typeof provisionCatalog;
  persist: (settings: Record<string, string>) => Promise<void>;
}

export async function setupStripe(deps: SetupDependencies) {
  await deps.preflight();
  const { key } = await deps.resolveKey();
  if (!/^(sk|rk)_test_[A-Za-z0-9_]+$/.test(key)) {
    throw new Error("Only Stripe sandbox/test-mode keys are accepted.");
  }
  // Authentication must succeed before provisioning catalog objects.
  const webhookSecret = await deps.webhookSecret(key);
  const { meter, price, portalConfiguration } = await deps.provision(key);
  await deps.persist({
    STRIPE_API_KEY: key,
    STRIPE_PRICE_ID_TUM: price.id,
    STRIPE_METER_ID_TUM: meter.id,
    STRIPE_METER_EVENT_NAME: METER_EVENT_NAME,
    STRIPE_PORTAL_CONFIGURATION_ID: portalConfiguration.id,
    STRIPE_WEBHOOK_SECRET: webhookSecret,
  });
}

/** Worktree-only safety: no database, organization or feature-flag access. */
export function preflightLocalSetup(
  root = process.cwd(),
  env = process.env,
): void {
  if (env.GRAM_ENVIRONMENT !== "local") {
    throw new Error("Stripe setup requires GRAM_ENVIRONMENT=local.");
  }
  localWebhookTarget(env.GRAM_SERVER_URL ?? "", env.GRAM_SERVER_PORT ?? "");
  try {
    // check-ignore also rejects tracked files, even when an ignore rule matches.
    execFileSync("git", ["check-ignore", "--quiet", "--", "mise.local.toml"], {
      cwd: root,
      stdio: "pipe",
    });
    try {
      const stat = lstatSync(join(root, "mise.local.toml"));
      if (!stat.isFile() || stat.nlink !== 1) throw new Error("unsafe file");
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error;
    }
  } catch {
    throw new Error(
      "Stripe setup requires an ignored, untracked regular mise.local.toml (no links).",
    );
  }
}

async function main() {
  intro("Stripe PAYG sandbox setup");
  await setupStripe({
    preflight: async () => {
      preflightLocalSetup();
      preflightStripeCLI();
    },
    resolveKey: resolveSecretKey,
    webhookSecret: resolveWebhookSecret,
    provision: provisionCatalog,
    persist: async (settings) => {
      preflightLocalSetup();
      for (const [name, value] of Object.entries(settings)) {
        await $({
          input: value,
          quiet: true,
        })`mise set --stdin --file mise.local.toml ${name}`;
      }
      if (process.env.usage_listen === "true") registerStripeListener();
      log.success(
        "Saved consistent test credentials and billing configuration to ignored mise.local.toml.",
      );
    },
  });

  const readiness = await getSetupReadiness();
  for (const check of readiness.checks)
    log.info(`${check.ok ? "OK" : "ACTION"} ${check.message}`);
  const commands = [
    ["mise run stripe:status", "Check configuration and forwarding"],
    ["mise run stripe:setup --listen", "Configure managed forwarding"],
    ["pitchfork start stripe-listener", "Start forwarding"],
    ["pitchfork restart server worker", "Reload saved billing credentials"],
    [
      "pitchfork restart stripe-listener",
      "Restart forwarding after config changes",
    ],
    ["pitchfork logs stripe-listener -n 50", "Inspect recent forwarding logs"],
    ["pitchfork stop stripe-listener", "Stop forwarding"],
  ] as const;
  const width = Math.max(...commands.map(([command]) => command.length));
  note(
    commands
      .map(
        ([command, description]) => `${command.padEnd(width)}  ${description}`,
      )
      .join("\n"),
    "Useful commands",
  );

  outro("Stripe configuration saved. See checks above for remaining actions.");
}

if (import.meta.main) {
  try {
    await main();
  } catch (err) {
    log.error(err instanceof Error ? err.message : String(err));
    process.exitCode = 1;
  }
}
