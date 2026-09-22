import assert from "node:assert/strict";
import { test } from "node:test";
import {
  provisionCatalog,
  preflightLocalSetup,
  setupStripe,
} from "./setup.mts";
import { execFileSync } from "node:child_process";
import {
  mkdtempSync,
  readFileSync,
  readdirSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const meter = {
  id: "mtr_synthetic",
  event_name: "tum",
  status: "active",
  livemode: false,
  default_aggregation: { formula: "sum" },
  value_settings: { event_payload_key: "value" },
  customer_mapping: { type: "by_id", event_payload_key: "stripe_customer_id" },
};
const price = {
  id: "price_synthetic",
  product: "prod_synthetic",
  active: true,
  livemode: false,
  currency: "usd",
  billing_scheme: "per_unit",
  unit_amount_decimal: "0.000035",
  recurring: {
    interval: "month",
    interval_count: 1,
    usage_type: "metered",
    meter: meter.id,
  },
};
const mcpEgressMeter = {
  ...meter,
  id: "mtr_egress",
  event_name: "existing_egress",
};
const riskScansMeter = {
  ...meter,
  id: "mtr_risk",
  event_name: "existing_risk",
  event_time_window: null,
};
const catalogMeters = [meter, mcpEgressMeter, riskScansMeter];
const mcpEgressPrice = {
  ...price,
  id: "price_mcp_egress",
  recurring: { ...price.recurring, meter: mcpEgressMeter.id },
};
const riskScansPrice = {
  ...price,
  id: "price_risk_scans",
  recurring: { ...price.recurring, meter: riskScansMeter.id },
};
const catalogPrices = {
  "payg-tum": price,
  "payg-mcp-egress": mcpEgressPrice,
  "payg-risk-scans": riskScansPrice,
};
const portal = {
  id: "bpc_synthetic",
  active: true,
  livemode: false,
  metadata: { purpose: "gram-payg" },
  features: {
    customer_update: { enabled: false },
    invoice_history: { enabled: true },
    payment_method_update: { enabled: true },
    subscription_update: { enabled: false },
    subscription_cancel: {
      enabled: true,
      mode: "at_period_end",
      proration_behavior: "none",
      cancellation_reason: { enabled: false },
    },
  },
};

for (const tag of ["aicp", "", "conflicting"]) {
  test(`catalog accepts mutable product names and protects metadata (${tag || "missing"})`, async (t) => {
    const writes: Array<{ path: string; params: URLSearchParams }> = [];
    t.mock.method(
      globalThis,
      "fetch",
      async (url: string, init: RequestInit) => {
        const path = new URL(url).pathname;
        const foundMeter = catalogMeters.find((item) =>
          path.endsWith(`/meters/${item.id}`),
        );
        if (foundMeter) return Response.json(foundMeter);
        if (init.method === "POST") {
          assert.ok(
            path.endsWith("/configurations") ||
              path.endsWith("/prod_synthetic"),
            "Unexpected write path",
          );
          writes.push({
            path,
            params: new URLSearchParams(init.body as string),
          });
          return Response.json(path.endsWith("configurations") ? portal : {});
        }
        if (path.endsWith("/account"))
          return Response.json({ livemode: false });
        if (path.endsWith("/meters"))
          return Response.json({ data: [meter], has_more: false });
        if (path.endsWith("/prices"))
          return Response.json({
            data: [
              catalogPrices[
                new URL(url).searchParams.get(
                  "lookup_keys[]",
                ) as keyof typeof catalogPrices
              ],
            ],
            has_more: false,
          });
        if (path.endsWith("/prod_synthetic"))
          return Response.json({
            id: "prod_synthetic",
            active: true,
            name: "Legacy PAYG display name",
            metadata: { speakeasy_product: tag, unrelated: "keep" },
          });
        if (path.endsWith("/configurations"))
          return Response.json({ data: [], has_more: false });
        throw new Error("Unexpected mocked request");
      },
    );
    if (tag === "conflicting") {
      await assert.rejects(
        provisionCatalog("sk_test_synthetic"),
        /unexpected metadata/,
      );
      assert.equal(writes.length, 0);
      return;
    }
    await provisionCatalog("sk_test_synthetic");
    const creation = writes.find((w) => w.path.endsWith("configurations"))!;
    assert.equal(
      creation.params.get(
        "features[subscription_cancel][cancellation_reason][enabled]",
      ),
      "false",
    );
    assert.equal(
      creation.params.get(
        "features[subscription_cancel][cancellation_reason][options][0]",
      ),
      "too_expensive",
    );
    assert.equal(
      creation.params.get(
        "features[subscription_cancel][cancellation_reason][options][1]",
      ),
      "other",
    );
    const productWrites = writes.filter((w) =>
      w.path.endsWith("prod_synthetic"),
    );
    assert.equal(productWrites.length, tag ? 0 : 1);
    if (!tag)
      assert.deepEqual(
        [...productWrites[0]!.params],
        [["metadata[speakeasy_product]", "aicp"]],
      );
  });
}

test("refuses non-test credentials before requests", async (t) => {
  const request = t.mock.method(globalThis, "fetch", () => {
    throw new Error("must not request");
  });
  for (const key of ["sk_live_synthetic", "unset", "not-a-key"]) {
    await assert.rejects(provisionCatalog(key), /test-mode/);
  }
  assert.equal(request.mock.callCount(), 0);
});

test("orchestration shares credentials and preflights before remote writes", async () => {
  const { setupStripe } = await import("./setup.mts");
  for (const source of ["env", "cli", "prompt"]) {
    const key = `sk_test_synthetic_${source}`;
    const steps: string[] = [];
    await setupStripe({
      preflight: async () => {
        steps.push("preflight");
      },
      resolveKey: async () => {
        steps.push("key");
        return { key };
      },
      webhookSecret: async (actual) => {
        assert.equal(actual, key);
        steps.push("listener");
        return "whsec_synthetic";
      },
      provision: async (actual) => {
        assert.equal(actual, key);
        steps.push("catalog");
        return {
          meter,
          mcpEgressMeter,
          riskScansMeter,
          price,
          mcpEgressPrice,
          riskScansPrice,
          portalConfiguration: portal,
        };
      },
      persist: async (settings) => {
        steps.push("persist");
        assert.deepEqual(Object.keys(settings).sort(), [
          "STRIPE_API_KEY",
          "STRIPE_METER_EVENT_NAME",
          "STRIPE_METER_EVENT_NAME_MCP_BANDWIDTH_EGRESS",
          "STRIPE_METER_EVENT_NAME_RISK_CLI_DESTRUCTIVE",
          "STRIPE_METER_EVENT_NAME_RISK_CUSTOM_RULES",
          "STRIPE_METER_EVENT_NAME_RISK_GITLEAKS",
          "STRIPE_METER_EVENT_NAME_RISK_LLM_ANALYZER",
          "STRIPE_METER_EVENT_NAME_RISK_PRESIDIO",
          "STRIPE_METER_EVENT_NAME_RISK_PROMPT_INJECTION",
          "STRIPE_METER_EVENT_NAME_RISK_PROMPT_POLICY",
          "STRIPE_METER_ID_TUM",
          "STRIPE_PORTAL_CONFIGURATION_ID",
          "STRIPE_PRICE_ID_MCP_EGRESS",
          "STRIPE_PRICE_ID_RISK_SCANS",
          "STRIPE_PRICE_ID_TUM",
          "STRIPE_WEBHOOK_SECRET",
        ]);
        assert.equal(
          settings.STRIPE_METER_EVENT_NAME_MCP_BANDWIDTH_EGRESS,
          "existing_egress",
        );
        for (const [name, value] of Object.entries(settings)) {
          if (name.startsWith("STRIPE_METER_EVENT_NAME_RISK_"))
            assert.equal(value, "existing_risk");
        }
        assert.equal(settings.STRIPE_API_KEY, key);
        assert.equal(settings.STRIPE_WEBHOOK_SECRET, "whsec_synthetic");
        assert.equal(settings.STRIPE_PRICE_ID_MCP_EGRESS, mcpEgressPrice.id);
        assert.equal(settings.STRIPE_PRICE_ID_RISK_SCANS, riskScansPrice.id);
      },
    });
    assert.deepEqual(steps, [
      "preflight",
      "key",
      "listener",
      "catalog",
      "persist",
    ]);
  }
  for (const failure of ["preflight", "key", "listener"]) {
    let writes = 0;
    await assert.rejects(
      setupStripe({
        preflight: async () => {
          if (failure === "preflight") throw new Error("CLI missing");
        },
        resolveKey: async () => ({
          key: failure === "key" ? "sk_live_synthetic" : "sk_test_synthetic",
        }),
        webhookSecret: async () => {
          throw new Error("listener authentication failed");
        },
        provision: async () => {
          writes++;
          return {
            meter,
            mcpEgressMeter,
            riskScansMeter,
            price,
            mcpEgressPrice,
            riskScansPrice,
            portalConfiguration: portal,
          };
        },
        persist: async () => {
          writes++;
        },
      }),
    );
    assert.equal(writes, 0);
  }
});

test("compatible existing catalog is write-free", async (t) => {
  t.mock.method(globalThis, "fetch", async (url: string, init: RequestInit) => {
    assert.equal(init.method, "GET");
    const path = new URL(url).pathname;
    const foundMeter = catalogMeters.find((item) =>
      path.endsWith(`/meters/${item.id}`),
    );
    if (foundMeter) return Response.json(foundMeter);
    if (path.endsWith("/account")) return Response.json({ livemode: false });
    if (path.endsWith("/meters"))
      return Response.json({ data: [meter], has_more: false });
    if (path.endsWith("/prices")) {
      const lookupKey = new URL(url).searchParams.get("lookup_keys[]");
      assert.equal(new URL(url).searchParams.get("active"), "true");
      const prices = {
        "payg-tum": price,
        "payg-mcp-egress": {
          ...mcpEgressPrice,
          billing_scheme: "tiered",
          unit_amount_decimal: "0.123",
        },
        "payg-risk-scans": riskScansPrice,
      };
      assert.ok(lookupKey && lookupKey in prices);
      return Response.json({
        data: [prices[lookupKey as keyof typeof prices]],
        has_more: false,
      });
    }
    if (path.endsWith("/prod_synthetic"))
      return Response.json({
        id: "prod_synthetic",
        active: true,
        name: "Legacy name",
        metadata: { speakeasy_product: "aicp" },
      });
    if (path.endsWith("/configurations"))
      return Response.json({ data: [portal], has_more: false });
    throw new Error("Unexpected request");
  });
  const result = await provisionCatalog("sk_test_synthetic");
  assert.equal(result.mcpEgressPrice.id, mcpEgressPrice.id);
  assert.equal(result.riskScansPrice.id, riskScansPrice.id);
  assert.equal(result.mcpEgressPrice.billing_scheme, "tiered");
  assert.equal(result.mcpEgressPrice.unit_amount_decimal, "0.123");
  assert.equal(result.mcpEgressMeter.event_name, "existing_egress");
  assert.equal(result.riskScansMeter.event_name, "existing_risk");
});

test("licensed risk scans price fails before catalog writes", async (t) => {
  t.mock.method(globalThis, "fetch", async (url: string, init: RequestInit) => {
    assert.equal(init.method, "GET");
    const request = new URL(url);
    if (request.pathname.endsWith(`/meters/${mcpEgressMeter.id}`))
      return Response.json(mcpEgressMeter);
    if (request.pathname.endsWith("/account"))
      return Response.json({ livemode: false });
    const found =
      request.searchParams.get("lookup_keys[]") === "payg-mcp-egress"
        ? mcpEgressPrice
        : {
            ...riskScansPrice,
            recurring: { ...price.recurring, usage_type: "licensed" },
          };
    return Response.json({ data: [found], has_more: false });
  });
  await assert.rejects(
    provisionCatalog("sk_test_synthetic"),
    /payg-risk-scans.*recurring.usage_type/,
  );
});

test("archived product fails before tagging or portal writes", async (t) => {
  t.mock.method(globalThis, "fetch", async (url: string, init: RequestInit) => {
    assert.equal(init.method, "GET");
    const path = new URL(url).pathname;
    const foundMeter = catalogMeters.find((item) =>
      path.endsWith(`/meters/${item.id}`),
    );
    if (foundMeter) return Response.json(foundMeter);
    if (path.endsWith("/account")) return Response.json({ livemode: false });
    if (path.endsWith("/meters"))
      return Response.json({ data: [meter], has_more: false });
    if (path.endsWith("/prices"))
      return Response.json({
        data: [
          catalogPrices[
            new URL(url).searchParams.get(
              "lookup_keys[]",
            ) as keyof typeof catalogPrices
          ],
        ],
        has_more: false,
      });
    if (path.endsWith("/prod_synthetic"))
      return Response.json({
        id: "prod_synthetic",
        active: false,
        name: "Archived",
        metadata: {},
      });
    throw new Error("Unexpected request");
  });
  await assert.rejects(provisionCatalog("sk_test_synthetic"), /active.*false/);
});

test("worktree preflight needs no org, CSV or database and protects ignored secrets", async (t) => {
  const root = mkdtempSync(join(tmpdir(), "gram-stripe-setup-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  execFileSync("git", ["init", "--quiet", root]);
  execFileSync("git", ["-C", root, "config", "core.excludesFile", "/dev/null"]);
  writeFileSync(join(root, ".gitignore"), "mise.local.toml\n");
  const env = {
    GRAM_ENVIRONMENT: "local",
    GRAM_SERVER_URL: "http://localhost:8080",
    GRAM_SERVER_PORT: "8080",
  };
  const before = readdirSync(root);
  preflightLocalSetup(root, env);
  assert.deepEqual(readdirSync(root), before);
  const config = "[env]\nEXISTING_SETTING = 'preserve'\n";
  writeFileSync(join(root, "mise.local.toml"), config);
  preflightLocalSetup(root, env);
  assert.equal(readFileSync(join(root, "mise.local.toml"), "utf8"), config);
  for (const unsafe of [
    { ...env, GRAM_ENVIRONMENT: "production" },
    { ...env, GRAM_ENVIRONMENT: undefined },
    { ...env, GRAM_SERVER_URL: "https://example.invalid" },
    { ...env, GRAM_SERVER_PORT: "8082" },
  ]) {
    let calls = 0;
    await assert.rejects(
      setupStripe({
        preflight: async () => preflightLocalSetup(root, unsafe),
        resolveKey: async () => {
          calls++;
          return { key: "sk_test_synthetic" };
        },
        webhookSecret: async () => {
          calls++;
          return "whsec_synthetic";
        },
        provision: async () => {
          calls++;
          return {
            meter,
            mcpEgressMeter,
            riskScansMeter,
            price,
            mcpEgressPrice,
            riskScansPrice,
            portalConfiguration: portal,
          };
        },
        persist: async () => {
          calls++;
        },
      }),
    );
    assert.equal(
      calls,
      0,
      "unsafe targets fail before CLI credentials or remote/local writes",
    );
  }
  writeFileSync(join(root, ".gitignore"), "");
  assert.throws(() => preflightLocalSetup(root, env), /ignored/);
  writeFileSync(join(root, ".gitignore"), "mise.local.toml\n");
  rmSync(join(root, "mise.local.toml"));
  symlinkSync(join(root, ".gitignore"), join(root, "mise.local.toml"));
  assert.throws(() => preflightLocalSetup(root, env), /no links/);
});

for (const inactiveMeters of [false, true]) {
  test(`sandbox bootstrap and rerun are idempotent (inactive meters: ${inactiveMeters})`, async (t) => {
    const meters = new Map<string, typeof meter>();
    const prices = new Map<string, typeof price>();
    const writes: Array<{ path: string; params: URLSearchParams }> = [];
    let portalCreated = false;
    if (inactiveMeters) {
      for (const event_name of ["mcp_egress", "risk_scans", "tum"]) {
        meters.set(`mtr_${event_name}`, {
          ...meter,
          id: `mtr_${event_name}`,
          event_name,
          status: "inactive",
        });
      }
    }
    t.mock.method(
      globalThis,
      "fetch",
      async (url: string, init: RequestInit) => {
        const request = new URL(url);
        const path = request.pathname.replace("/v1", "");
        const params = new URLSearchParams(init.body as string);
        if (init.method === "POST") writes.push({ path, params });
        if (path === "/account") return Response.json({ livemode: false });
        if (path === "/billing/meters") {
          if (init.method === "POST") {
            const event_name = params.get("event_name")!;
            assert.equal(params.get("default_aggregation[formula]"), "sum");
            assert.equal(
              params.get("value_settings[event_payload_key]"),
              "value",
            );
            assert.equal(params.get("customer_mapping[type]"), "by_id");
            assert.equal(
              params.get("customer_mapping[event_payload_key]"),
              "stripe_customer_id",
            );
            const created = { ...meter, id: `mtr_${event_name}`, event_name };
            assert.ok(!meters.has(created.id));
            meters.set(created.id, created);
            return Response.json(created);
          }
          return Response.json({
            data: [...meters.values()].filter(
              (m) => m.status === request.searchParams.get("status"),
            ),
            has_more: false,
          });
        }
        if (path.startsWith("/billing/meters/")) {
          const found = meters.get(path.split("/")[3]!);
          assert.ok(found);
          if (path.endsWith("/reactivate")) found.status = "active";
          return Response.json(found);
        }
        if (path === "/prices") {
          if (init.method === "POST") {
            const lookup = params.get("lookup_key")!;
            assert.ok(!prices.has(lookup));
            const created = {
              ...price,
              id: `price_${lookup}`,
              unit_amount_decimal: params.get("unit_amount_decimal")!,
              recurring: {
                ...price.recurring,
                meter: params.get("recurring[meter]")!,
              },
            };
            assert.equal(params.get("currency"), "usd");
            assert.equal(params.get("billing_scheme"), "per_unit");
            assert.equal(params.get("recurring[interval]"), "month");
            assert.equal(params.get("recurring[usage_type]"), "metered");
            assert.equal(params.has("transform_quantity[divide_by]"), false);
            prices.set(lookup, created);
            return Response.json(created);
          }
          const found = prices.get(request.searchParams.get("lookup_keys[]")!);
          return Response.json({
            data:
              found && request.searchParams.get("active") === "true"
                ? [found]
                : [],
            has_more: false,
          });
        }
        if (path === "/products/prod_synthetic")
          return Response.json({
            id: "prod_synthetic",
            active: true,
            name: "AI Control Plane PAYG",
            metadata: { speakeasy_product: "aicp" },
          });
        if (path === "/billing_portal/configurations") {
          if (init.method === "POST") {
            portalCreated = true;
            return Response.json(portal);
          }
          return Response.json({
            data: portalCreated ? [portal] : [],
            has_more: false,
          });
        }
        throw new Error(`Unexpected mock request: ${path}`);
      },
    );
    const first = await provisionCatalog("sk_test_synthetic");
    assert.equal(first.mcpEgressMeter.event_name, "mcp_egress");
    assert.equal(first.riskScansMeter.event_name, "risk_scans");
    assert.equal(first.mcpEgressPrice.unit_amount_decimal, "0.000001862645");
    assert.equal(first.riskScansPrice.unit_amount_decimal, "0.000099");
    assert.equal(first.price.unit_amount_decimal, "0.000035");
    assert.equal(meters.size, 3);
    assert.equal(prices.size, 3);
    assert.equal(
      writes.filter((w) => w.path === "/billing/meters").length,
      inactiveMeters ? 0 : 3,
    );
    assert.equal(
      writes.filter((w) => w.path.endsWith("/reactivate")).length,
      inactiveMeters ? 3 : 0,
    );
    const before = writes.length;
    assert.deepEqual(await provisionCatalog("rk_test_synthetic"), first);
    assert.equal(
      writes.length,
      before,
      "rerun must not create or modify anything",
    );
  });
}

for (const invalid of [
  { event_time_window: "hour" },
  { event_time_window: "day" },
  { status: "inactive" },
  { livemode: true },
  { default_aggregation: { formula: "count" } },
  { value_settings: { event_payload_key: "bytes" } },
  { customer_mapping: { type: "by_id", event_payload_key: "customer" } },
  { event_name: "" },
]) {
  test(`rejects existing price's invalid meter: ${JSON.stringify(invalid)}`, async (t) => {
    t.mock.method(
      globalThis,
      "fetch",
      async (url: string, init: RequestInit) => {
        assert.equal(
          init.method,
          "GET",
          "invalid existing objects must not be changed",
        );
        const path = new URL(url).pathname;
        if (path.endsWith("/account"))
          return Response.json({ livemode: false });
        if (path.endsWith("/prices"))
          return Response.json({ data: [mcpEgressPrice], has_more: false });
        if (path.endsWith(`/meters/${mcpEgressMeter.id}`))
          return Response.json({ ...mcpEgressMeter, ...invalid });
        throw new Error("Unexpected request");
      },
    );
    await assert.rejects(
      provisionCatalog("sk_test_synthetic"),
      /misconfigured/,
    );
  });
}

test("archived additional price is never replaced", async (t) => {
  t.mock.method(globalThis, "fetch", async (url: string, init: RequestInit) => {
    assert.equal(init.method, "GET");
    const request = new URL(url);
    if (request.pathname.endsWith("/account"))
      return Response.json({ livemode: false });
    assert.ok(request.pathname.endsWith("/prices"));
    return Response.json({
      data:
        request.searchParams.get("active") === "false"
          ? [{ ...mcpEgressPrice, active: false }]
          : [],
      has_more: false,
    });
  });
  await assert.rejects(provisionCatalog("sk_test_synthetic"), /Archived price/);
});

for (const status of ["active", "inactive"]) {
  for (const invalid of [
    { default_aggregation: { formula: "count" } },
    { event_time_window: "hour" },
    { event_time_window: "day" },
  ]) {
    test(`missing price rejects incompatible ${status} meter ${JSON.stringify(invalid)} before writes`, async (t) => {
      t.mock.method(
        globalThis,
        "fetch",
        async (url: string, init: RequestInit) => {
          assert.equal(init.method, "GET");
          const request = new URL(url);
          if (request.pathname.endsWith("/account"))
            return Response.json({ livemode: false });
          if (request.pathname.endsWith("/prices"))
            return Response.json({ data: [], has_more: false });
          if (request.pathname.endsWith("/meters"))
            return Response.json({
              data:
                request.searchParams.get("status") === status
                  ? [
                      {
                        ...meter,
                        status,
                        event_name: "mcp_egress",
                        ...invalid,
                      },
                    ]
                  : [],
              has_more: false,
            });
          throw new Error("Unexpected request");
        },
      );
      await assert.rejects(
        provisionCatalog("sk_test_synthetic"),
        /default_aggregation.formula|event_time_window/,
      );
    });
  }
}

for (const [first, second] of [
  [0, 1],
  [0, 2],
  [1, 2],
] as const) {
  for (const field of ["id", "event_name"] as const) {
    test(`rejects shared meter ${field} between categories ${first} and ${second} without persistence`, async () => {
      const meters = catalogMeters.map((item) => ({ ...item }));
      meters[second]![field] = meters[first]![field];
      let persisted = false;
      await assert.rejects(
        setupStripe({
          preflight: async () => {},
          resolveKey: async () => ({ key: "sk_test_synthetic" }),
          webhookSecret: async () => "whsec_synthetic",
          provision: async () => ({
            meter: meters[0]!,
            mcpEgressMeter: meters[1]!,
            riskScansMeter: meters[2]!,
            price,
            mcpEgressPrice,
            riskScansPrice,
            portalConfiguration: portal,
          }),
          persist: async () => {
            persisted = true;
          },
        }),
        new RegExp(`distinct meter ${field}`),
      );
      assert.equal(persisted, false);
    });
  }
}
