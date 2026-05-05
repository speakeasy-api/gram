#!/usr/bin/env node
/**
 * Recall-side embedding cost + performance bench.
 *
 * Locks-in fact: at recall time the query embedding model MUST match the
 * write-side embedding model, otherwise cosine is meaningless. So this
 * bench is not a free per-recall model selection — the non-Qwen3-8B cells
 * answer "what would we save by re-embedding everything to a cheaper
 * model?", weighed against a fresh dedup re-calibration cost.
 *
 * Matrix: 5 candidate models × 3 query-shape buckets × N queries.
 * Per cell: p50 / p95 / p99 latency, mean input tokens, cost per 1M
 * recalls at OpenRouter list pricing.
 *
 * Usage:
 *   pnpm bench-recall-embed
 *   EMBED_MODELS=qwen/qwen3-embedding-8b,qwen/qwen3-embedding-4b pnpm bench-recall-embed
 *   PER_MODEL_CONCURRENCY=4 pnpm bench-recall-embed
 *   RECALL_EMBED_OUTPUT_PATH=./recall-embed.json pnpm bench-recall-embed
 *   WARMUP_CALLS=2 pnpm bench-recall-embed
 *   MONTHLY_RECALLS=10000000 pnpm bench-recall-embed
 *
 * See README.md for interpretation guidance.
 */

import OpenAI from "openai";
import { writeFileSync } from "node:fs";
import { allQueries, type Bucket, type Query } from "./fixture.ts";

const OPENROUTER_BASE_URL = "https://openrouter.ai/api/v1";

const DEFAULT_MODELS = [
  "qwen/qwen3-embedding-8b",
  "qwen/qwen3-embedding-4b",
  "baai/bge-large-en-v1.5",
  "openai/text-embedding-3-large",
  "openai/text-embedding-3-small",
];

/**
 * USD per 1M input tokens. Embeddings have no output tokens. List prices
 * from OpenRouter as of late-2025; verify at procurement time. Override
 * via EMBED_PRICING_JSON env var if stale.
 */
const DEFAULT_PRICING_USD_PER_1M_INPUT_TOKENS: Record<string, number> = {
  "qwen/qwen3-embedding-8b": 0.07,
  "qwen/qwen3-embedding-4b": 0.05,
  "baai/bge-large-en-v1.5": 0.01,
  "openai/text-embedding-3-large": 0.13,
  "openai/text-embedding-3-small": 0.02,
};

const PER_MODEL_CONCURRENCY = Number(process.env.PER_MODEL_CONCURRENCY ?? 8);
const PER_CALL_TIMEOUT_MS = Number(process.env.PER_CALL_TIMEOUT_MS ?? 30_000);
const WARMUP_CALLS = Number(process.env.WARMUP_CALLS ?? 2);
const MONTHLY_RECALLS = Number(process.env.MONTHLY_RECALLS ?? 10_000_000);

type CallResult = {
  model: string;
  bucket: Bucket;
  query: Query;
  latencyMs: number;
  inputTokens: number;
  dimensions: number;
  error?: string;
};

type ModelResult = {
  model: string;
  calls: CallResult[];
  fatalError?: string;
};

type Pricing = Record<string, number>;

function loadPricing(): Pricing {
  const override = process.env.EMBED_PRICING_JSON;
  if (!override) return DEFAULT_PRICING_USD_PER_1M_INPUT_TOKENS;
  try {
    const parsed = JSON.parse(override);
    return { ...DEFAULT_PRICING_USD_PER_1M_INPUT_TOKENS, ...parsed };
  } catch (e) {
    console.error(
      `EMBED_PRICING_JSON is not valid JSON, ignoring: ${(e as Error).message}`,
    );
    return DEFAULT_PRICING_USD_PER_1M_INPUT_TOKENS;
  }
}

async function embedOnce(
  client: OpenAI,
  model: string,
  text: string,
): Promise<{ latencyMs: number; inputTokens: number; dimensions: number }> {
  const t0 = Date.now();
  const ctrl = new AbortController();
  const timeout = setTimeout(() => ctrl.abort(), PER_CALL_TIMEOUT_MS);
  try {
    const result = await client.embeddings.create(
      { model, input: text },
      { signal: ctrl.signal },
    );
    const latencyMs = Date.now() - t0;
    const inputTokens = result.usage?.prompt_tokens ?? 0;
    const dimensions =
      (result.data[0]?.embedding as number[] | undefined)?.length ?? 0;
    return { latencyMs, inputTokens, dimensions };
  } finally {
    clearTimeout(timeout);
  }
}

async function runWithConcurrency<T, R>(
  items: T[],
  concurrency: number,
  fn: (item: T) => Promise<R>,
): Promise<R[]> {
  const results: R[] = new Array(items.length);
  let next = 0;
  async function worker(): Promise<void> {
    while (true) {
      const idx = next++;
      if (idx >= items.length) return;
      results[idx] = await fn(items[idx]);
    }
  }
  await Promise.all(
    Array.from({ length: Math.min(concurrency, items.length) }, worker),
  );
  return results;
}

async function benchModel(client: OpenAI, model: string): Promise<ModelResult> {
  // Warmup: discard first N calls to avoid cold-route bias.
  if (WARMUP_CALLS > 0) {
    try {
      const sample = allQueries.slice(0, WARMUP_CALLS).map((q) => q.query.text);
      await Promise.all(
        sample.map((t) => embedOnce(client, model, t).catch(() => null)),
      );
    } catch {
      // Warmup failures are non-fatal; the real run will surface fatal errors.
    }
  }

  try {
    const calls = await runWithConcurrency(
      allQueries,
      PER_MODEL_CONCURRENCY,
      async ({ bucket, query }): Promise<CallResult> => {
        try {
          const { latencyMs, inputTokens, dimensions } = await embedOnce(
            client,
            model,
            query.text,
          );
          return { model, bucket, query, latencyMs, inputTokens, dimensions };
        } catch (e) {
          return {
            model,
            bucket,
            query,
            latencyMs: -1,
            inputTokens: 0,
            dimensions: 0,
            error: (e as Error).message ?? String(e),
          };
        }
      },
    );
    return { model, calls };
  } catch (e) {
    return { model, calls: [], fatalError: (e as Error).message ?? String(e) };
  }
}

function quantile(arr: number[], q: number): number {
  if (arr.length === 0) return NaN;
  const sorted = [...arr].sort((a, b) => a - b);
  const i = Math.min(sorted.length - 1, Math.floor((sorted.length - 1) * q));
  return sorted[i];
}

function mean(arr: number[]): number {
  return arr.length === 0 ? NaN : arr.reduce((a, b) => a + b, 0) / arr.length;
}

type Cell = {
  model: string;
  bucket: Bucket | "all";
  count: number;
  errors: number;
  p50Ms: number;
  p95Ms: number;
  p99Ms: number;
  meanInputTokens: number;
  costPerMillion: number;
  dimensions: number;
};

function summarize(result: ModelResult, pricing: Pricing): Cell[] {
  const buckets: (Bucket | "all")[] = ["short", "medium", "long", "all"];
  const out: Cell[] = [];
  const price = pricing[result.model] ?? NaN;
  for (const bucket of buckets) {
    const calls = result.calls.filter(
      (c) => bucket === "all" || c.bucket === bucket,
    );
    const ok = calls.filter((c) => !c.error);
    const lat = ok.map((c) => c.latencyMs);
    const tok = ok.map((c) => c.inputTokens);
    const dims = ok.map((c) => c.dimensions).filter((d) => d > 0);
    const meanTok = mean(tok);
    const costPerMillion = Number.isFinite(price) ? meanTok * price : NaN;
    out.push({
      model: result.model,
      bucket,
      count: ok.length,
      errors: calls.length - ok.length,
      p50Ms: quantile(lat, 0.5),
      p95Ms: quantile(lat, 0.95),
      p99Ms: quantile(lat, 0.99),
      meanInputTokens: meanTok,
      costPerMillion,
      dimensions: dims.length ? dims[0] : 0,
    });
  }
  return out;
}

function fmtMs(ms: number): string {
  if (!Number.isFinite(ms)) return "—";
  return ms >= 1000 ? `${(ms / 1000).toFixed(2)}s` : `${ms.toFixed(0)}`;
}

function fmtCost(usd: number): string {
  if (!Number.isFinite(usd)) return "—";
  if (usd === 0) return "$0";
  if (usd < 1) return `$${usd.toFixed(3)}`;
  if (usd < 100) return `$${usd.toFixed(2)}`;
  return `$${usd.toFixed(0)}`;
}

function pad(s: string, n: number): string {
  return s.length >= n ? s : s + " ".repeat(n - s.length);
}

function lpad(s: string, n: number): string {
  return s.length >= n ? s : " ".repeat(n - s.length) + s;
}

function printSummaryTable(cells: Cell[]): void {
  // Sort all-cells by cost/1M (asc).
  const allCells = cells.filter((c) => c.bucket === "all");
  allCells.sort(
    (a, b) => (a.costPerMillion || Infinity) - (b.costPerMillion || Infinity),
  );

  console.log("\n=== Cross-model summary (all queries) ===");
  console.log(
    pad("model", 36) +
      " | " +
      lpad("dim", 5) +
      " | " +
      lpad("p50", 5) +
      " | " +
      lpad("p95", 5) +
      " | " +
      lpad("p99", 5) +
      " | " +
      lpad("mean tok", 9) +
      " | " +
      lpad("$/1M recalls", 13) +
      " | " +
      lpad("err", 4),
  );
  console.log(
    "-".repeat(36) +
      "-+-" +
      "-".repeat(5) +
      "-+-" +
      "-".repeat(5) +
      "-+-" +
      "-".repeat(5) +
      "-+-" +
      "-".repeat(5) +
      "-+-" +
      "-".repeat(9) +
      "-+-" +
      "-".repeat(13) +
      "-+-" +
      "-".repeat(4),
  );
  for (const c of allCells) {
    console.log(
      pad(c.model, 36) +
        " | " +
        lpad(c.dimensions ? String(c.dimensions) : "—", 5) +
        " | " +
        lpad(fmtMs(c.p50Ms), 5) +
        " | " +
        lpad(fmtMs(c.p95Ms), 5) +
        " | " +
        lpad(fmtMs(c.p99Ms), 5) +
        " | " +
        lpad(c.meanInputTokens ? c.meanInputTokens.toFixed(1) : "—", 9) +
        " | " +
        lpad(fmtCost(c.costPerMillion), 13) +
        " | " +
        lpad(String(c.errors), 4),
    );
  }
}

function printPerShapeTable(cells: Cell[]): void {
  const byModel = new Map<string, Cell[]>();
  for (const c of cells) {
    if (!byModel.has(c.model)) byModel.set(c.model, []);
    byModel.get(c.model)!.push(c);
  }
  for (const [model, modelCells] of byModel) {
    console.log(`\n--- ${model} (per query shape) ---`);
    console.log(
      pad("bucket", 10) +
        " | " +
        lpad("n", 4) +
        " | " +
        lpad("p50", 5) +
        " | " +
        lpad("p95", 5) +
        " | " +
        lpad("p99", 5) +
        " | " +
        lpad("mean tok", 9) +
        " | " +
        lpad("$/1M", 9),
    );
    console.log(
      "-".repeat(10) +
        "-+-" +
        "-".repeat(4) +
        "-+-" +
        "-".repeat(5) +
        "-+-" +
        "-".repeat(5) +
        "-+-" +
        "-".repeat(5) +
        "-+-" +
        "-".repeat(9) +
        "-+-" +
        "-".repeat(9),
    );
    for (const c of modelCells) {
      console.log(
        pad(c.bucket, 10) +
          " | " +
          lpad(String(c.count), 4) +
          " | " +
          lpad(fmtMs(c.p50Ms), 5) +
          " | " +
          lpad(fmtMs(c.p95Ms), 5) +
          " | " +
          lpad(fmtMs(c.p99Ms), 5) +
          " | " +
          lpad(c.meanInputTokens ? c.meanInputTokens.toFixed(1) : "—", 9) +
          " | " +
          lpad(fmtCost(c.costPerMillion), 9),
      );
    }
  }
}

function printMonthlyProjection(cells: Cell[], monthlyRecalls: number): void {
  const all = cells.filter((c) => c.bucket === "all");
  console.log(
    `\n=== Monthly cost projection at ${monthlyRecalls.toLocaleString()} recalls/month ===`,
  );
  console.log(pad("model", 36) + " | " + lpad("$/month", 12));
  console.log("-".repeat(36) + "-+-" + "-".repeat(12));
  const sorted = [...all].sort(
    (a, b) => (a.costPerMillion || Infinity) - (b.costPerMillion || Infinity),
  );
  for (const c of sorted) {
    const monthly = (c.costPerMillion / 1_000_000) * monthlyRecalls;
    console.log(pad(c.model, 36) + " | " + lpad(fmtCost(monthly), 12));
  }
}

async function main(): Promise<void> {
  const apiKey = process.env.OPENROUTER_API_KEY;
  if (!apiKey) {
    console.error(
      "OPENROUTER_API_KEY is not set. Add it to evaluation/.env or your shell.",
    );
    process.exit(1);
  }

  const modelsArg = process.env.EMBED_MODELS;
  const models = modelsArg
    ? modelsArg
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean)
    : DEFAULT_MODELS;
  const pricing = loadPricing();

  const client = new OpenAI({ apiKey, baseURL: OPENROUTER_BASE_URL });

  console.log(
    `Bench: ${models.length} models × ${allQueries.length} queries (${PER_MODEL_CONCURRENCY} concurrent per model, ${WARMUP_CALLS} warmup calls).`,
  );
  console.log("Models:");
  for (const m of models) {
    const price = pricing[m];
    console.log(
      `  - ${m}${price !== undefined ? ` ($${price}/1M input tokens)` : " (no pricing — cost cells will be —)"}`,
    );
  }

  const t0 = Date.now();
  const results = await Promise.all(models.map((m) => benchModel(client, m)));
  const wall = ((Date.now() - t0) / 1000).toFixed(1);

  const allCells: Cell[] = [];
  for (const r of results) {
    if (r.fatalError) {
      console.log(`\n[!] ${r.model} fatal: ${r.fatalError}`);
      continue;
    }
    allCells.push(...summarize(r, pricing));
  }

  printSummaryTable(allCells);
  printPerShapeTable(allCells);
  printMonthlyProjection(allCells, MONTHLY_RECALLS);

  // Per-model error sample.
  for (const r of results) {
    const errs = r.calls.filter((c) => c.error);
    if (errs.length > 0) {
      console.log(`\n--- ${r.model}: ${errs.length} errors (first 3) ---`);
      for (const e of errs.slice(0, 3))
        console.log(`  [${e.bucket}/${e.query.id}] ${e.error}`);
    }
  }

  const dumpPath = process.env.RECALL_EMBED_OUTPUT_PATH;
  if (dumpPath) {
    writeFileSync(
      dumpPath,
      JSON.stringify(
        {
          models,
          pricing,
          monthlyRecalls: MONTHLY_RECALLS,
          perModelConcurrency: PER_MODEL_CONCURRENCY,
          warmupCalls: WARMUP_CALLS,
          results: results.map((r) => ({
            model: r.model,
            fatalError: r.fatalError ?? null,
            calls: r.calls.map((c) => ({
              bucket: c.bucket,
              queryId: c.query.id,
              latencyMs: c.latencyMs,
              inputTokens: c.inputTokens,
              dimensions: c.dimensions,
              error: c.error ?? null,
            })),
          })),
          cells: allCells,
        },
        null,
        2,
      ),
    );
    console.log(`\nRaw dump written to ${dumpPath}`);
  }

  console.log(`\nWall: ${wall}s.`);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
