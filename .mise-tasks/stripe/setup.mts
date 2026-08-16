#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Provision and validate Stripe sandbox billing objects and initialize the local webhook secret"

// Idempotent: the meter is keyed on its event name and the price on its
// lookup key, so re-running against a sandbox that already has the objects
// just re-saves the existing IDs. Safe to run any time; refuses live-mode
// keys outright.

import { intro, isCancel, log, outro, password } from "@clack/prompts";
import { $ } from "zx";

const METER_EVENT_NAME = "tum";
const METER_DISPLAY_NAME = "Tokens under management";
const PRICE_LOOKUP_KEY = "payg-tum";
const PRODUCT_NAME = "AI Control Plane PAYG";
const PORTAL_CONFIGURATION_PURPOSE = "gram-payg";
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

interface StripePrice {
  id: string;
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
      `Stripe ${method} ${path} failed (${res.status}): ${body.error?.message ?? JSON.stringify(body)}`,
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
  // for provisioning but never persist it — it expires after ~90 days.
  const cliConfig = await $({
    nothrow: true,
    quiet: true,
  })`stripe config --list`;
  const cliKey = cliConfig.stdout.match(
    /test_mode_api_key\s*=\s*['"]?((?:sk|rk)_test_[A-Za-z0-9_]+)/,
  )?.[1];
  if (cliKey) {
    log.info("Using the test-mode key from the authenticated Stripe CLI.");
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
    const details = output
      .replaceAll(key, "<redacted>")
      .replace(/(?:sk|rk)_(?:test|live)_[A-Za-z0-9_]+/g, "<redacted>")
      .replace(/whsec_[A-Za-z0-9]+/g, "<redacted>")
      .trim();
    throw new Error(
      `Initialize Stripe CLI webhook listener secret failed${details ? `: ${details}` : "."}`,
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

async function main() {
  intro("Stripe PAYG sandbox setup");

  const { key, prompted } = await resolveSecretKey();
  if (/^(sk|rk)_live_/.test(key)) {
    log.error("Refusing to run against a live-mode key.");
    process.exit(1);
  }

  const account = await stripe<StripeAccount>(key, "GET", "/account");
  if (account.livemode === true) {
    log.error("Refusing to run against a live-mode Stripe account.");
    process.exit(1);
  }
  const accountLabel =
    account.settings?.dashboard?.display_name ?? account.id ?? "unknown";
  log.info(`Connected to Stripe account: ${accountLabel}`);

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
        "features[subscription_update][enabled]": "false",
      },
    );
    log.success(
      `Created Billing Portal configuration "${PORTAL_CONFIGURATION_PURPOSE}": ${portalConfiguration.id}`,
    );
  }
  assertPortalConfiguration(portalConfiguration);

  const webhookSecret = await resolveWebhookSecret(key);
  log.success("Initialized the Stripe CLI webhook signing secret.");

  const settings: Record<string, string> = {
    STRIPE_PRICE_ID_TUM: price.id,
    STRIPE_METER_ID_TUM: meter.id,
    STRIPE_METER_EVENT_NAME: METER_EVENT_NAME,
    STRIPE_PORTAL_CONFIGURATION_ID: portalConfiguration.id,
    STRIPE_WEBHOOK_SECRET: webhookSecret,
  };
  if (prompted) {
    settings.STRIPE_API_KEY = key;
  }
  for (const [name, value] of Object.entries(settings)) {
    await $({
      input: value,
      quiet: true,
    })`mise set --stdin --file mise.local.toml ${name}`;
  }
  log.success(`Saved to mise.local.toml: ${Object.keys(settings).join(", ")}`);

  log.info(
    [
      "Start webhook forwarding for this worktree in a separate terminal:",
      '  if test "${STRIPE_API_KEY:-}" = unset; then unset STRIPE_API_KEY; fi',
      '  stripe listen --latest --skip-verify --forward-to "$GRAM_SERVER_URL/rpc/stripe.webhook"',
    ].join("\n"),
  );

  log.info(
    [
      "Finish Stripe Dashboard setup before sandbox validation:",
      "",
      "1. Configure subscription Smart Retries",
      "   Billing → Revenue recovery → Retries",
      "   - Under Card payments, click Manage.",
      "   - Select Smart Retries: 8 retries within 2 weeks.",
      "   - Set Subscription status to cancel the subscription.",
      "   - Leave Invoice status as leave the invoice past-due; it applies to one-off invoices.",
      "   - Save the settings.",
      "",
      "2. Configure the metered invoice grace period",
      "   Settings → Billing → Invoices → Invoice finalization grace period",
      "   - Click Add rule and set Invoice finalization delay to 72 hours.",
      "   - Add both conditions: Has a metered price and Invoice is from a subscription cycle.",
      "   - Save the rule.",
    ].join("\n"),
  );

  outro("Done.");
}

try {
  await main();
} catch (err) {
  log.error(err instanceof Error ? err.message : String(err));
  process.exit(1);
}
