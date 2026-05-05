#!/usr/bin/env node
/**
 * Dedup-cosine calibration probe across multiple embedding models on OpenRouter.
 *
 * Embeds the labelled fixture under each candidate model, sweeps four
 * similarity variants × nine thresholds × three buckets, and reports a
 * cross-model summary (best knee per model) + detailed tables for the
 * top performer.
 *
 * Usage:
 *   pnpm calibrate-dedup                                   # default model list
 *   EMBED_MODELS=qwen/qwen3-embedding-8b,... pnpm calibrate-dedup
 *   VERBOSE=1 pnpm calibrate-dedup                         # full tables for every model
 *   CALIBRATION_OUTPUT_PATH=./scores.json pnpm calibrate-dedup  # raw dump
 *
 * See README.md for interpretation guidance.
 */

import OpenAI from "openai";
import {
  unrelated,
  trueDuplicate,
  relatedDistinct,
  type Pair,
} from "./fixture.ts";

const OPENROUTER_BASE_URL = "https://openrouter.ai/api/v1";

const DEFAULT_MODELS = [
  "openai/text-embedding-3-small",
  "openai/text-embedding-3-large",
  "qwen/qwen3-embedding-8b",
  "qwen/qwen3-embedding-4b",
  "google/gemini-embedding-001",
  "mistralai/mistral-embed-2312",
  "baai/bge-large-en-v1.5",
];

type Bucket = "unrelated" | "true-duplicate" | "related-distinct";
type LabeledPair = Pair & { bucket: Bucket };

const allPairs: LabeledPair[] = [
  ...unrelated.map((p) => ({ ...p, bucket: "unrelated" as const })),
  ...trueDuplicate.map((p) => ({ ...p, bucket: "true-duplicate" as const })),
  ...relatedDistinct.map((p) => ({
    ...p,
    bucket: "related-distinct" as const,
  })),
];

function normalize(s: string): string {
  return s
    .toLowerCase()
    .replace(/[^a-z0-9 ]/g, " ")
    .replace(/\s+/g, " ")
    .trim();
}

function stripSubject(s: string): string {
  return s
    .replace(/^\s*[Tt]he\s+user('s)?\s+/, "")
    .replace(/^\s*[Uu]ser('s)?\s+/, "")
    .trim();
}

function trigrams(s: string): Set<string> {
  const padded = `  ${s.toLowerCase()}  `;
  const grams = new Set<string>();
  for (let i = 0; i <= padded.length - 3; i++)
    grams.add(padded.slice(i, i + 3));
  return grams;
}

function jaccard(a: string, b: string): number {
  const A = trigrams(a);
  const B = trigrams(b);
  if (A.size === 0 || B.size === 0) return 0;
  let inter = 0;
  for (const g of A) if (B.has(g)) inter++;
  return inter / (A.size + B.size - inter);
}

/** L2-normalize a vector in place. Some embedders return non-unit vectors. */
function l2Normalize(v: number[]): number[] {
  let s = 0;
  for (const x of v) s += x * x;
  const n = Math.sqrt(s) || 1;
  return v.map((x) => x / n);
}

/** Cosine via dot product on L2-normalized vectors. */
function cosine(a: number[], b: number[]): number {
  let s = 0;
  for (let i = 0; i < a.length; i++) s += a[i] * b[i];
  return s;
}

async function embedBatch(
  client: OpenAI,
  model: string,
  texts: string[],
): Promise<Map<string, number[]>> {
  const unique = Array.from(new Set(texts));
  const result = await client.embeddings.create({ model, input: unique });
  const out = new Map<string, number[]>();
  unique.forEach((t, i) =>
    out.set(t, l2Normalize(result.data[i].embedding as number[])),
  );
  return out;
}

function quantile(arr: number[], q: number): number {
  if (arr.length === 0) return NaN;
  const sorted = [...arr].sort((a, b) => a - b);
  const i = Math.floor((sorted.length - 1) * q);
  return sorted[i];
}

type VariantKey = "raw" | "norm" | "strip" | "jac" | "composite";

type Score = LabeledPair & Record<VariantKey, number>;

type ModelResult = {
  model: string;
  scores: Score[];
  bestKnee: {
    variant: VariantKey;
    threshold: number;
    trueDupRate: number;
    unrelatedRate: number;
    relatedDistinctRate: number;
    separation: number;
  };
  embedMs: number;
  error?: string;
};

const THRESHOLDS = [
  0.99, 0.97, 0.95, 0.92, 0.9, 0.88, 0.85, 0.82, 0.8, 0.75, 0.7, 0.65, 0.6,
];

function computeBestKnee(scores: Score[]): ModelResult["bestKnee"] {
  const variants: VariantKey[] = ["raw", "norm", "strip", "composite"];
  const counts: Record<Bucket, number> = {
    unrelated: scores.filter((s) => s.bucket === "unrelated").length,
    "true-duplicate": scores.filter((s) => s.bucket === "true-duplicate")
      .length,
    "related-distinct": scores.filter((s) => s.bucket === "related-distinct")
      .length,
  };

  let best: ModelResult["bestKnee"] = {
    variant: "raw",
    threshold: 1,
    trueDupRate: 0,
    unrelatedRate: 0,
    relatedDistinctRate: 0,
    separation: -1,
  };
  for (const v of variants) {
    for (const t of THRESHOLDS) {
      const td =
        scores.filter((s) => s.bucket === "true-duplicate" && s[v] >= t)
          .length / counts["true-duplicate"];
      const un =
        scores.filter((s) => s.bucket === "unrelated" && s[v] >= t).length /
        counts.unrelated;
      const rd =
        scores.filter((s) => s.bucket === "related-distinct" && s[v] >= t)
          .length / counts["related-distinct"];
      const sep = td - Math.max(un, rd);
      // Require true-dup rate to be non-trivial (>= 50%) to count as a viable knee.
      if (sep > best.separation && td >= 0.5) {
        best = {
          variant: v,
          threshold: t,
          trueDupRate: td,
          unrelatedRate: un,
          relatedDistinctRate: rd,
          separation: sep,
        };
      }
    }
  }
  return best;
}

async function runForModel(
  client: OpenAI,
  model: string,
): Promise<ModelResult> {
  const t0 = Date.now();
  try {
    const rawTexts = allPairs.flatMap((p) => [p.a, p.b]);
    const normTexts = rawTexts.map(normalize);
    const stripTexts = rawTexts.map(stripSubject);

    const [rawEmb, normEmb, stripEmb] = await Promise.all([
      embedBatch(client, model, rawTexts),
      embedBatch(client, model, normTexts),
      embedBatch(client, model, stripTexts),
    ]);

    const scores: Score[] = allPairs.map((p) => {
      const raw = cosine(rawEmb.get(p.a)!, rawEmb.get(p.b)!);
      const norm = cosine(
        normEmb.get(normalize(p.a))!,
        normEmb.get(normalize(p.b))!,
      );
      const strip = cosine(
        stripEmb.get(stripSubject(p.a))!,
        stripEmb.get(stripSubject(p.b))!,
      );
      const jac = jaccard(p.a, p.b);
      const composite = Math.max(raw, jac);
      return { ...p, raw, norm, strip, jac, composite };
    });

    return {
      model,
      scores,
      bestKnee: computeBestKnee(scores),
      embedMs: Date.now() - t0,
    };
  } catch (err) {
    return {
      model,
      scores: [],
      bestKnee: {
        variant: "raw",
        threshold: 0,
        trueDupRate: 0,
        unrelatedRate: 0,
        relatedDistinctRate: 0,
        separation: -1,
      },
      embedMs: Date.now() - t0,
      error: err instanceof Error ? err.message : String(err),
    };
  }
}

function printCrossModelSummary(results: ModelResult[]): void {
  const ok = results.filter((r) => !r.error);
  const failed = results.filter((r) => r.error);
  ok.sort((a, b) => b.bestKnee.separation - a.bestKnee.separation);

  console.log("\n## Cross-model summary — best knee per model");
  console.log(
    "Sorted by separation (higher = cleaner band gap between paraphrases and related-distinct).\n",
  );
  console.log(
    "model                                |  variant   |   thr | true-dup | unrelated | rel-dist | separation |  ms",
  );
  console.log(
    "-------------------------------------|------------|------:|---------:|----------:|---------:|-----------:|----:",
  );
  for (const r of ok) {
    const k = r.bestKnee;
    console.log(
      `${r.model.padEnd(36)} | ${k.variant.padEnd(10)} | ${k.threshold.toFixed(2)} | ${(
        k.trueDupRate * 100
      )
        .toFixed(1)
        .padStart(
          7,
        )}% | ${(k.unrelatedRate * 100).toFixed(1).padStart(8)}% | ${(
        k.relatedDistinctRate * 100
      )
        .toFixed(1)
        .padStart(
          7,
        )}% | ${(k.separation * 100).toFixed(1).padStart(9)}% | ${String(
        r.embedMs,
      ).padStart(4)}`,
    );
  }
  if (failed.length > 0) {
    console.log("\n## Failed models");
    for (const r of failed) {
      console.log(`  ${r.model}: ${r.error}`);
    }
  }
  console.log();
}

function printDistribution(scores: Score[], header: string): void {
  const variants: VariantKey[] = ["raw", "norm", "strip", "jac", "composite"];
  const buckets: Bucket[] = ["unrelated", "related-distinct", "true-duplicate"];

  console.log(`\n## Per-bucket score distribution — ${header}`);
  console.log(
    "Goal: `true-duplicate` clearly above `unrelated` AND `related-distinct`.\n",
  );
  console.log(
    "variant    | bucket            |   n |   min |    p5 |   p50 |   p95 |   max",
  );
  console.log(
    "-----------|-------------------|----:|------:|------:|------:|------:|------:",
  );
  for (const v of variants) {
    for (const b of buckets) {
      const vals = scores.filter((s) => s.bucket === b).map((s) => s[v]);
      const row = [
        Math.min(...vals),
        quantile(vals, 0.05),
        quantile(vals, 0.5),
        quantile(vals, 0.95),
        Math.max(...vals),
      ];
      console.log(
        `${v.padEnd(10)} | ${b.padEnd(17)} | ${String(vals.length).padStart(3)} | ${row
          .map((x) => x.toFixed(3).padStart(5))
          .join(" | ")}`,
      );
    }
    console.log();
  }
}

function printSweep(scores: Score[], header: string): void {
  const variants: VariantKey[] = ["raw", "norm", "strip", "composite"];
  const counts: Record<Bucket, number> = {
    unrelated: scores.filter((s) => s.bucket === "unrelated").length,
    "true-duplicate": scores.filter((s) => s.bucket === "true-duplicate")
      .length,
    "related-distinct": scores.filter((s) => s.bucket === "related-distinct")
      .length,
  };

  console.log(`## Sweep — ${header}\n`);
  console.log(
    "variant    |  thr | true-dup | unrelated | related-distinct | separation",
  );
  console.log(
    "-----------|-----:|---------:|----------:|-----------------:|----------:",
  );
  for (const v of variants) {
    for (const t of THRESHOLDS) {
      const td =
        scores.filter((s) => s.bucket === "true-duplicate" && s[v] >= t)
          .length / counts["true-duplicate"];
      const un =
        scores.filter((s) => s.bucket === "unrelated" && s[v] >= t).length /
        counts.unrelated;
      const rd =
        scores.filter((s) => s.bucket === "related-distinct" && s[v] >= t)
          .length / counts["related-distinct"];
      const sep = td - Math.max(un, rd);
      console.log(
        `${v.padEnd(10)} | ${t.toFixed(2)} | ${(td * 100)
          .toFixed(1)
          .padStart(7)}% | ${(un * 100).toFixed(1).padStart(8)}% | ${(rd * 100)
          .toFixed(1)
          .padStart(15)}% | ${(sep * 100).toFixed(1).padStart(8)}%`,
      );
    }
    console.log();
  }
}

function printDangerExamples(scores: Score[], header: string): void {
  console.log(
    `## Top-10 highest-cosine \`related-distinct\` (the trap zone) — ${header}\n`,
  );
  const danger = scores
    .filter((s) => s.bucket === "related-distinct")
    .sort((a, b) => b.raw - a.raw)
    .slice(0, 10);
  for (const s of danger) {
    console.log(
      `  raw=${s.raw.toFixed(3)} norm=${s.norm.toFixed(3)} strip=${s.strip.toFixed(3)}  "${s.a}" / "${s.b}"`,
    );
  }
  console.log(
    `\n## Bottom-10 lowest-cosine \`true-duplicate\` (the miss zone) — ${header}\n`,
  );
  const miss = scores
    .filter((s) => s.bucket === "true-duplicate")
    .sort((a, b) => a.raw - b.raw)
    .slice(0, 10);
  for (const s of miss) {
    console.log(
      `  raw=${s.raw.toFixed(3)} norm=${s.norm.toFixed(3)} strip=${s.strip.toFixed(3)}  "${s.a}" / "${s.b}"`,
    );
  }
  console.log();
}

async function main(): Promise<void> {
  const apiKey = process.env.OPENROUTER_API_KEY;
  if (!apiKey) {
    console.error(
      "Error: OPENROUTER_API_KEY environment variable is required.",
    );
    process.exit(1);
  }

  const models = (process.env.EMBED_MODELS || DEFAULT_MODELS.join(","))
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);

  const verbose = process.env.VERBOSE === "1";

  const client = new OpenAI({ apiKey, baseURL: OPENROUTER_BASE_URL });

  process.stderr.write(
    `Calibrating against ${models.length} embedding model(s) via OpenRouter\n`,
  );
  process.stderr.write(
    `Fixture: ${unrelated.length} unrelated + ${trueDuplicate.length} true-dup + ${relatedDistinct.length} related-distinct = ${allPairs.length} pairs\n`,
  );
  process.stderr.write(`Models: ${models.join(", ")}\n\n`);

  // Run all models in parallel.
  const results = await Promise.all(
    models.map(async (m) => {
      process.stderr.write(`  → ${m} starting\n`);
      const r = await runForModel(client, m);
      process.stderr.write(
        `  ← ${m} ${r.error ? `FAILED: ${r.error}` : `done in ${r.embedMs}ms`}\n`,
      );
      return r;
    }),
  );

  printCrossModelSummary(results);

  const ok = results.filter((r) => !r.error);
  ok.sort((a, b) => b.bestKnee.separation - a.bestKnee.separation);

  if (ok.length === 0) {
    console.log("All models failed. Check error messages above.");
    process.exit(1);
  }

  // Detailed tables for the winner (and all others if VERBOSE).
  const detailed = verbose ? ok : [ok[0]];
  for (const r of detailed) {
    console.log(`\n# Detailed report — ${r.model}\n`);
    printDistribution(r.scores, r.model);
    printSweep(r.scores, r.model);
    printDangerExamples(r.scores, r.model);
  }

  if (!verbose && ok.length > 1) {
    console.log(
      `(detailed tables shown for top model only; set VERBOSE=1 for all ${ok.length})`,
    );
  }

  const outPath = process.env.CALIBRATION_OUTPUT_PATH;
  if (outPath) {
    const fs = await import("node:fs/promises");
    await fs.writeFile(
      outPath,
      JSON.stringify({ thresholds: THRESHOLDS, results }, null, 2),
    );
    process.stderr.write(`\nRaw scores written to ${outPath}\n`);
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
