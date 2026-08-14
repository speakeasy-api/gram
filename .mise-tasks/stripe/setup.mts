#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Provision and validate the Stripe sandbox TUM meter and metered price, then save the catalog config to mise.local.toml"

// Idempotent: the meter is keyed on its event name and the price on its
// lookup key, so re-running against a sandbox that already has the objects
// just re-saves the existing IDs. Safe to run any time; refuses live-mode
// keys outright.

import { intro, isCancel, log, outro, password } from "@clack/prompts";
import { $ } from "zx";

const STRIPE_API_VERSION = "2026-03-25.dahlia";

const METER_EVENT_NAME = "tum";
const METER_DISPLAY_NAME = "Tokens under management";
const PRICE_LOOKUP_KEY = "payg-tum";
const PRODUCT_NAME = "AI Control Plane PAYG";
// $0.35 per 1M TUMs, linear per-unit, expressed in cents per TUM.
const UNIT_AMOUNT_DECIMAL_CENTS = "0.000035";

interface StripeError {
  error?: { message?: string; type?: string };
}

function assertConfiguration(
  objectName: string,
  checks: Array<[field: string, actual: unknown, expected: unknown]>,
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
    `${objectName} exists but is misconfigured: ${details}. ` +
      "Archive the object in the Stripe sandbox and re-run this task.",
  );
}

async function stripe(
  key: string,
  method: "GET" | "POST",
  path: string,
  params?: Record<string, string>,
): Promise<any> {
  let url = `https://api.stripe.com/v1${path}`;
  const init: RequestInit = {
    method,
    headers: {
      Authorization: `Bearer ${key}`,
      "Stripe-Version": STRIPE_API_VERSION,
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
  const body = (await res.json()) as StripeError;
  if (!res.ok) {
    throw new Error(
      `Stripe ${method} ${path} failed (${res.status}): ${body.error?.message ?? JSON.stringify(body)}`,
    );
  }
  return body;
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
    /test_mode_api_key\s*=\s*'?((?:sk|rk)_test_[A-Za-z0-9_]+)/,
  )?.[1];
  if (cliKey) {
    log.info("Using the test-mode key from the authenticated Stripe CLI.");
    return { key: cliKey, prompted: false };
  }

  if (!process.stdin.isTTY) {
    log.error(
      "STRIPE_API_KEY is not set and there is no terminal to prompt on. " +
        "Set it in mise.local.toml (`mise set --file mise.local.toml STRIPE_API_KEY=sk_test_…`) and re-run.",
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

async function main() {
  intro("Stripe PAYG sandbox setup");

  const { key, prompted } = await resolveSecretKey();
  if (/^(sk|rk)_live_/.test(key)) {
    log.error("Refusing to run against a live-mode key.");
    process.exit(1);
  }

  const account = await stripe(key, "GET", "/account");
  if (account.livemode === true) {
    log.error("Refusing to run against a live-mode Stripe account.");
    process.exit(1);
  }
  const accountLabel =
    account.settings?.dashboard?.display_name ?? account.id ?? "unknown";
  log.info(`Connected to Stripe account: ${accountLabel}`);

  // Meter — event names are unique among active meters, so match on that.
  const meters = await stripe(key, "GET", "/billing/meters", {
    status: "active",
    limit: "100",
  });
  let meter = meters.data?.find((m: any) => m.event_name === METER_EVENT_NAME);
  if (meter) {
    log.info(`Meter "${METER_EVENT_NAME}" already exists: ${meter.id}`);
  } else {
    meter = await stripe(key, "POST", "/billing/meters", {
      display_name: METER_DISPLAY_NAME,
      event_name: METER_EVENT_NAME,
      "default_aggregation[formula]": "sum",
      "value_settings[event_payload_key]": "value",
      "customer_mapping[type]": "by_id",
      "customer_mapping[event_payload_key]": "stripe_customer_id",
    });
    log.success(`Created meter "${METER_EVENT_NAME}": ${meter.id}`);
  }
  assertConfiguration(`Meter ${meter.id}`, [
    ["status", meter.status, "active"],
    ["livemode", meter.livemode, false],
    ["default_aggregation.formula", meter.default_aggregation?.formula, "sum"],
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
  ]);

  // Price and product — keyed on the lookup key. Creating them in one request
  // avoids leaving an orphan product if price creation fails.
  const prices = await stripe(key, "GET", "/prices", {
    "lookup_keys[]": PRICE_LOOKUP_KEY,
    active: "true",
    limit: "1",
  });
  let price = prices.data?.[0];
  if (price) {
    log.info(`Price "${PRICE_LOOKUP_KEY}" already exists: ${price.id}`);
  } else {
    price = await stripe(key, "POST", "/prices", {
      "product_data[name]": PRODUCT_NAME,
      lookup_key: PRICE_LOOKUP_KEY,
      nickname: "PAYG TUM ($0.35 per 1M)",
      currency: "usd",
      billing_scheme: "per_unit",
      unit_amount_decimal: UNIT_AMOUNT_DECIMAL_CENTS,
      "recurring[interval]": "month",
      "recurring[usage_type]": "metered",
      "recurring[meter]": meter.id,
    });
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

  const settings: Record<string, string> = {
    STRIPE_PRICE_ID_TUM: price.id,
    STRIPE_METER_EVENT_NAME: METER_EVENT_NAME,
  };
  if (prompted) {
    settings.STRIPE_API_KEY = key;
  }
  const pairs = Object.entries(settings).map(([k, v]) => `${k}=${v}`);
  await $`mise set --file mise.local.toml ${pairs}`;
  log.success(`Saved to mise.local.toml: ${Object.keys(settings).join(", ")}`);

  log.info(
    [
      "Manual follow-ups (dashboard-only settings, not settable via API):",
      "  - Billing → Revenue recovery: use 8 Smart Retry attempts over 2 weeks,",
      "    then cancel the subscription after retries are exhausted.",
      "  - Billing → Invoices: set a 72-hour finalization grace period for this metered price.",
      "  - Customer emails: enable failed-payment, finalized-invoice, and receipt emails.",
      "  - Webhooks: once the webhook route exists, run `stripe listen --forward-to` against it",
      "    and put the printed whsec_… value in mise.local.toml as STRIPE_WEBHOOK_SECRET.",
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
