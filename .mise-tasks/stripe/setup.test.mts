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
const mcpEgressPrice = { ...price, id: "price_mcp_egress" };
const riskScansPrice = { ...price, id: "price_risk_scans" };
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
          return Response.json({ data: [price], has_more: false });
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
          "STRIPE_METER_ID_TUM",
          "STRIPE_PORTAL_CONFIGURATION_ID",
          "STRIPE_PRICE_ID_MCP_EGRESS",
          "STRIPE_PRICE_ID_RISK_SCANS",
          "STRIPE_PRICE_ID_TUM",
          "STRIPE_WEBHOOK_SECRET",
        ]);
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
    if (path.endsWith("/account")) return Response.json({ livemode: false });
    if (path.endsWith("/meters"))
      return Response.json({ data: [meter], has_more: false });
    if (path.endsWith("/prices")) {
      const lookupKey = new URL(url).searchParams.get("lookup_keys[]");
      assert.equal(new URL(url).searchParams.get("active"), "true");
      const prices = {
        "payg-tum": price,
        "payg-mcp-egress": mcpEgressPrice,
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
});

test("missing additional price fails before catalog writes", async (t) => {
  t.mock.method(globalThis, "fetch", async (url: string, init: RequestInit) => {
    assert.equal(init.method, "GET");
    const request = new URL(url);
    if (request.pathname.endsWith("/account"))
      return Response.json({ livemode: false });
    assert.equal(request.searchParams.get("lookup_keys[]"), "payg-mcp-egress");
    return Response.json({ data: [], has_more: false });
  });
  await assert.rejects(
    provisionCatalog("sk_test_synthetic"),
    /No active Stripe sandbox price.*payg-mcp-egress/,
  );
});

test("licensed risk scans price fails before catalog writes", async (t) => {
  t.mock.method(globalThis, "fetch", async (url: string, init: RequestInit) => {
    assert.equal(init.method, "GET");
    const request = new URL(url);
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
    if (path.endsWith("/account")) return Response.json({ livemode: false });
    if (path.endsWith("/meters"))
      return Response.json({ data: [meter], has_more: false });
    if (path.endsWith("/prices"))
      return Response.json({ data: [price], has_more: false });
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
