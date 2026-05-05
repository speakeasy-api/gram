#!/usr/bin/env node
/**
 * Contradiction-detection model bench.
 *
 * Runs every (model, pair) cell against the labelled fixture, sweeps
 * confidence thresholds, picks the best knee per model by F1 on the
 * `contradicts: true` label.
 *
 * Outputs:
 *   1. Cross-model summary (best F1 per model + latency + tokens)
 *   2. Per-model confusion matrix at best threshold
 *   3. Cosine baseline (illustrative — see README)
 *   4. Optional raw dump via CONTRADICTION_OUTPUT_PATH
 *
 * Usage:
 *   pnpm bench-contradiction
 *   MODELS=anthropic/claude-haiku-4-5 pnpm bench-contradiction
 *   PER_MODEL_CONCURRENCY=4 pnpm bench-contradiction
 */

import OpenAI from "openai";
import {
  contradicting,
  refining,
  extending,
  unrelated,
  type Pair,
} from "./fixture.ts";
import { PROMPT_VARIANTS, type PromptVariant, userPrompt } from "./prompt.ts";

const OPENROUTER_BASE_URL = "https://openrouter.ai/api/v1";

const DEFAULT_MODELS = [
  "anthropic/claude-haiku-4-5",
  "~openai/gpt-mini-latest",
  "~google/gemini-flash-latest",
  "qwen/qwen3.6-flash",
  "qwen/qwen3.6-35b-a3b",
  "deepseek/deepseek-v4-flash",
  "mistralai/mistral-medium-3-5",
];

type Bucket = "contradicting" | "refining" | "extending" | "unrelated";
type LabeledPair = Pair & { bucket: Bucket; gold: boolean };

const allPairs: LabeledPair[] = [
  ...contradicting.map((p) => ({
    ...p,
    bucket: "contradicting" as const,
    gold: true,
  })),
  ...refining.map((p) => ({ ...p, bucket: "refining" as const, gold: false })),
  ...extending.map((p) => ({
    ...p,
    bucket: "extending" as const,
    gold: false,
  })),
  ...unrelated.map((p) => ({
    ...p,
    bucket: "unrelated" as const,
    gold: false,
  })),
];

const PER_MODEL_CONCURRENCY = Number(process.env.PER_MODEL_CONCURRENCY ?? 8);
const PER_CALL_TIMEOUT_MS = Number(process.env.PER_CALL_TIMEOUT_MS ?? 30_000);

type Verdict = { contradicts: boolean; confidence: number };

type CallResult = {
  pair: LabeledPair;
  verdict: Verdict | null;
  rawOutput: string;
  latencyMs: number;
  promptTokens: number;
  completionTokens: number;
  parseError?: string;
};

type ModelResult = {
  model: string;
  calls: CallResult[];
  totalMs: number;
  fatalError?: string;
};

async function callOnce(
  client: OpenAI,
  model: string,
  variant: PromptVariant,
  pair: LabeledPair,
): Promise<CallResult> {
  const t0 = Date.now();
  const ctrl = new AbortController();
  const timeout = setTimeout(() => ctrl.abort(), PER_CALL_TIMEOUT_MS);

  try {
    const completion = await client.chat.completions.create(
      {
        model,
        messages: [
          { role: "system", content: variant.system },
          { role: "user", content: userPrompt(pair.a, pair.b) },
        ],
        response_format: variant.schema as never,
        temperature: 0,
        max_tokens: 200,
      },
      { signal: ctrl.signal },
    );
    const latencyMs = Date.now() - t0;
    const raw = completion.choices[0]?.message?.content ?? "";
    const promptTokens = completion.usage?.prompt_tokens ?? 0;
    const completionTokens = completion.usage?.completion_tokens ?? 0;

    let verdict: Verdict | null = null;
    let parseError: string | undefined;
    try {
      const cleaned = raw.replace(/```json\n?|\n?```/g, "").trim();
      const parsed = JSON.parse(cleaned);
      if (
        typeof parsed === "object" &&
        parsed !== null &&
        typeof parsed.contradicts === "boolean"
      ) {
        const confidence = variant.hasConfidence
          ? typeof parsed.confidence === "number"
            ? parsed.confidence
            : NaN
          : 0.95;
        if (variant.hasConfidence && Number.isNaN(confidence)) {
          parseError = "shape mismatch";
        } else {
          verdict = { contradicts: parsed.contradicts, confidence };
        }
      } else {
        parseError = "shape mismatch";
      }
    } catch (e) {
      parseError = e instanceof Error ? e.message : String(e);
    }

    return {
      pair,
      verdict,
      rawOutput: raw,
      latencyMs,
      promptTokens,
      completionTokens,
      parseError,
    };
  } catch (err) {
    return {
      pair,
      verdict: null,
      rawOutput: "",
      latencyMs: Date.now() - t0,
      promptTokens: 0,
      completionTokens: 0,
      parseError: err instanceof Error ? err.message : String(err),
    };
  } finally {
    clearTimeout(timeout);
  }
}

async function runForModel(
  client: OpenAI,
  model: string,
  variant: PromptVariant,
): Promise<ModelResult> {
  const t0 = Date.now();
  const calls: CallResult[] = new Array(allPairs.length);
  let next = 0;

  async function worker() {
    while (true) {
      const i = next++;
      if (i >= allPairs.length) return;
      calls[i] = await callOnce(client, model, variant, allPairs[i]);
    }
  }

  try {
    await Promise.all(
      Array.from({ length: PER_MODEL_CONCURRENCY }, () => worker()),
    );
    return {
      model: `${model}[${variant.name}]`,
      calls,
      totalMs: Date.now() - t0,
    };
  } catch (err) {
    return {
      model: `${model}[${variant.name}]`,
      calls: calls.filter(Boolean),
      totalMs: Date.now() - t0,
      fatalError: err instanceof Error ? err.message : String(err),
    };
  }
}

const CONFIDENCE_THRESHOLDS = [0.0, 0.5, 0.6, 0.7, 0.75, 0.8, 0.85, 0.9, 0.95];

type Score = {
  threshold: number;
  truePos: number;
  falsePos: number;
  trueNeg: number;
  falseNeg: number;
  parseFails: number;
  precision: number;
  recall: number;
  f1: number;
};

function scoreAt(calls: CallResult[], threshold: number): Score {
  let tp = 0;
  let fp = 0;
  let tn = 0;
  let fn = 0;
  let pf = 0;

  for (const c of calls) {
    if (!c.verdict) {
      pf++;
      // Parse failures count against the gold label: a missed contradiction
      // is a false-negative; a missed non-contradiction is a true-negative
      // (we conservatively didn't supersede). This mirrors production:
      // when the LLM call fails, we fall back to Create.
      if (c.pair.gold) fn++;
      else tn++;
      continue;
    }
    const predicted =
      c.verdict.contradicts && c.verdict.confidence >= threshold;
    if (predicted && c.pair.gold) tp++;
    else if (predicted && !c.pair.gold) fp++;
    else if (!predicted && c.pair.gold) fn++;
    else tn++;
  }

  const precision = tp + fp === 0 ? 0 : tp / (tp + fp);
  const recall = tp + fn === 0 ? 0 : tp / (tp + fn);
  const f1 =
    precision + recall === 0
      ? 0
      : (2 * precision * recall) / (precision + recall);

  return {
    threshold,
    truePos: tp,
    falsePos: fp,
    trueNeg: tn,
    falseNeg: fn,
    parseFails: pf,
    precision,
    recall,
    f1,
  };
}

function quantile(arr: number[], q: number): number {
  if (arr.length === 0) return 0;
  const sorted = [...arr].sort((a, b) => a - b);
  const i = Math.floor((sorted.length - 1) * q);
  return sorted[i];
}

function bestKnee(calls: CallResult[]): Score {
  let best: Score = scoreAt(calls, 0);
  for (const t of CONFIDENCE_THRESHOLDS) {
    const s = scoreAt(calls, t);
    if (s.f1 > best.f1) best = s;
  }
  return best;
}

async function runCosineBaseline(client: OpenAI): Promise<{
  scores: Array<{ pair: LabeledPair; cosine: number }>;
  embedMs: number;
}> {
  const t0 = Date.now();
  const texts = Array.from(new Set(allPairs.flatMap((p) => [p.a, p.b])));
  const result = await client.embeddings.create({
    model: "qwen/qwen3-embedding-8b",
    input: texts,
  });
  const byText = new Map<string, number[]>();
  texts.forEach((t, i) => {
    const v = result.data[i].embedding as number[];
    let s = 0;
    for (const x of v) s += x * x;
    const n = Math.sqrt(s) || 1;
    byText.set(
      t,
      v.map((x) => x / n),
    );
  });

  const scores = allPairs.map((p) => {
    const a = byText.get(p.a)!;
    const b = byText.get(p.b)!;
    let dot = 0;
    for (let i = 0; i < a.length; i++) dot += a[i] * b[i];
    return { pair: p, cosine: dot };
  });

  return { scores, embedMs: Date.now() - t0 };
}

function scoreCosineBand(
  scores: Array<{ pair: LabeledPair; cosine: number }>,
  low: number,
  high: number,
): Score {
  let tp = 0;
  let fp = 0;
  let tn = 0;
  let fn = 0;
  for (const { pair, cosine } of scores) {
    const predicted = cosine >= low && cosine < high;
    if (predicted && pair.gold) tp++;
    else if (predicted && !pair.gold) fp++;
    else if (!predicted && pair.gold) fn++;
    else tn++;
  }
  const precision = tp + fp === 0 ? 0 : tp / (tp + fp);
  const recall = tp + fn === 0 ? 0 : tp / (tp + fn);
  const f1 =
    precision + recall === 0
      ? 0
      : (2 * precision * recall) / (precision + recall);
  return {
    threshold: low,
    truePos: tp,
    falsePos: fp,
    trueNeg: tn,
    falseNeg: fn,
    parseFails: 0,
    precision,
    recall,
    f1,
  };
}

function printCrossModelSummary(
  results: ModelResult[],
  cosineRows: Array<{ band: string; score: Score }>,
): void {
  console.log("\n## Cross-model summary — best confidence threshold per model");
  console.log("Sorted by F1 on `contradicts: true` label.\n");
  console.log(
    "model                                    | best_thr | precision | recall |  F1   | p50 ms | p95 ms | parse_fail | tokens",
  );
  console.log(
    "-----------------------------------------|---------:|----------:|-------:|------:|-------:|-------:|-----------:|------:",
  );

  const ok = results.filter(
    (r) => !r.fatalError && r.calls.some((c) => c.verdict),
  );
  const failed = results.filter(
    (r) => r.fatalError || !r.calls.some((c) => c.verdict),
  );

  const rows = ok.map((r) => {
    const knee = bestKnee(r.calls);
    const lats = r.calls.map((c) => c.latencyMs);
    const tokens = r.calls.reduce(
      (s, c) => s + c.promptTokens + c.completionTokens,
      0,
    );
    return {
      r,
      knee,
      p50: quantile(lats, 0.5),
      p95: quantile(lats, 0.95),
      tokens,
    };
  });
  rows.sort((a, b) => b.knee.f1 - a.knee.f1);

  for (const { r, knee, p50, p95, tokens } of rows) {
    console.log(
      `${r.model.padEnd(40)} | ${knee.threshold.toFixed(2).padStart(8)} | ${(
        knee.precision * 100
      )
        .toFixed(1)
        .padStart(
          8,
        )}% | ${(knee.recall * 100).toFixed(1).padStart(5)}% | ${knee.f1
        .toFixed(3)
        .padStart(
          5,
        )} | ${String(p50).padStart(6)} | ${String(p95).padStart(6)} | ${String(
        knee.parseFails,
      ).padStart(10)} | ${String(tokens).padStart(6)}`,
    );
  }

  // Cosine baseline rows (illustrative — see README §"Why cosine alone fails").
  console.log(
    "-----------------------------------------|---------:|----------:|-------:|------:|-------:|-------:|-----------:|------:",
  );
  for (const { band, score } of cosineRows) {
    console.log(
      `cosine baseline (illustrative) ${band.padEnd(10)} |        — | ${(
        score.precision * 100
      )
        .toFixed(1)
        .padStart(
          8,
        )}% | ${(score.recall * 100).toFixed(1).padStart(5)}% | ${score.f1
        .toFixed(3)
        .padStart(
          5,
        )} | ${"~5".padStart(6)} | ${"~10".padStart(6)} | ${"—".padStart(
        10,
      )} | ${"—".padStart(6)}`,
    );
  }

  if (failed.length > 0) {
    console.log("\n## Failed models");
    for (const r of failed) {
      console.log(`  ${r.model}: ${r.fatalError ?? "all calls failed"}`);
      const sample = r.calls.find((c) => c.parseError);
      if (sample) console.log(`    sample error: ${sample.parseError}`);
    }
  }
  console.log();
}

function printPerModelDetail(r: ModelResult): void {
  const knee = bestKnee(r.calls);
  console.log(`\n## Detailed report — ${r.model}\n`);
  console.log(
    `Best knee: threshold=${knee.threshold} F1=${knee.f1.toFixed(3)} precision=${knee.precision.toFixed(3)} recall=${knee.recall.toFixed(3)}\n`,
  );

  console.log("### Confusion matrix at best threshold\n");
  console.log(`                 | predicted: contradicts | predicted: not |`);
  console.log(`-----------------|-----------------------:|---------------:|`);
  console.log(
    `gold: contradicts | ${String(knee.truePos).padStart(22)} | ${String(knee.falseNeg).padStart(14)} |`,
  );
  console.log(
    `gold: not         | ${String(knee.falsePos).padStart(22)} | ${String(knee.trueNeg).padStart(14)} |`,
  );
  console.log();

  console.log("### Per-bucket false-positive breakdown");
  const buckets: Bucket[] = ["refining", "extending", "unrelated"];
  for (const b of buckets) {
    const n = r.calls.filter((c) => c.pair.bucket === b).length;
    const fp = r.calls.filter(
      (c) =>
        c.pair.bucket === b &&
        c.verdict?.contradicts &&
        c.verdict.confidence >= knee.threshold,
    ).length;
    console.log(`  ${b.padEnd(11)} → ${fp}/${n} false-positive`);
  }
  console.log();

  console.log("### Worst false-positives (would erroneously Supersede)\n");
  const fps = r.calls.filter(
    (c) =>
      c.verdict?.contradicts &&
      !c.pair.gold &&
      c.verdict.confidence >= knee.threshold,
  );
  fps
    .sort((a, b) => b.verdict!.confidence - a.verdict!.confidence)
    .slice(0, 8)
    .forEach((c) => {
      console.log(
        `  conf=${c.verdict!.confidence.toFixed(2)} bucket=${c.pair.bucket}  "${c.pair.a}" / "${c.pair.b}"`,
      );
    });
  console.log();

  console.log("### Worst false-negatives (would miss Supersede)\n");
  const fns = r.calls.filter(
    (c) =>
      c.pair.gold &&
      (!c.verdict?.contradicts || c.verdict.confidence < knee.threshold),
  );
  fns.slice(0, 8).forEach((c) => {
    const v = c.verdict;
    const note = v
      ? `verdict={contradicts:${v.contradicts}, conf:${v.confidence.toFixed(2)}}`
      : `parse_fail (${c.parseError})`;
    console.log(`  ${note}  "${c.pair.a}" / "${c.pair.b}"`);
  });
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

  const models = (process.env.MODELS || DEFAULT_MODELS.join(","))
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);

  const variantNames = (
    process.env.PROMPT_VARIANTS || PROMPT_VARIANTS.map((v) => v.name).join(",")
  )
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);
  const variants = PROMPT_VARIANTS.filter((v) => variantNames.includes(v.name));
  if (variants.length === 0) {
    console.error(
      `Error: no prompt variants matched (asked for: ${variantNames.join(",")})`,
    );
    process.exit(1);
  }

  const verbose = process.env.VERBOSE === "1";
  const skipCosine = process.env.SKIP_COSINE === "1";

  const client = new OpenAI({ apiKey, baseURL: OPENROUTER_BASE_URL });

  process.stderr.write(
    `Benching ${models.length} model(s) × ${variants.length} prompt variant(s) on ${allPairs.length} pairs (concurrency=${PER_MODEL_CONCURRENCY}/cell)\n`,
  );
  process.stderr.write(
    `Buckets: ${contradicting.length} contradicting + ${refining.length} refining + ${extending.length} extending + ${unrelated.length} unrelated\n`,
  );
  process.stderr.write(`Models:   ${models.join(", ")}\n`);
  process.stderr.write(
    `Variants: ${variants.map((v) => v.name).join(", ")}\n\n`,
  );

  // Cosine baseline runs in parallel with model bench (unless skipped).
  const cosinePromise = skipCosine
    ? Promise.resolve({
        scores: [] as Array<{ pair: LabeledPair; cosine: number }>,
        embedMs: 0,
      })
    : (process.stderr.write(
        `  → cosine baseline (qwen/qwen3-embedding-8b) starting\n`,
      ),
      runCosineBaseline(client).catch((err) => ({
        scores: [] as Array<{ pair: LabeledPair; cosine: number }>,
        embedMs: 0,
        error: err instanceof Error ? err.message : String(err),
      })));

  // Each (model, variant) cell is its own bench run.
  const cellPromises = models.flatMap((m) =>
    variants.map(async (v) => {
      const cellName = `${m}[${v.name}]`;
      process.stderr.write(`  → ${cellName} starting\n`);
      const r = await runForModel(client, m, v);
      const ok = r.calls.filter((c) => c.verdict).length;
      process.stderr.write(
        `  ← ${cellName} done in ${r.totalMs}ms (${ok}/${r.calls.length} parsed)\n`,
      );
      return r;
    }),
  );

  const [cosineResult, ...results] = await Promise.all([
    cosinePromise,
    ...cellPromises,
  ]);

  // Cosine baseline rows (one per illustrative band).
  const cosineRows: Array<{ band: string; score: Score }> = [];
  if (cosineResult.scores.length > 0) {
    cosineRows.push(
      {
        band: "[0.65,0.92)",
        score: scoreCosineBand(cosineResult.scores, 0.65, 0.92),
      },
      {
        band: "[0.70,0.92)",
        score: scoreCosineBand(cosineResult.scores, 0.7, 0.92),
      },
      {
        band: "[0.75,0.92)",
        score: scoreCosineBand(cosineResult.scores, 0.75, 0.92),
      },
    );
    process.stderr.write(
      `  ← cosine baseline embedded in ${cosineResult.embedMs}ms\n\n`,
    );
  } else if (skipCosine) {
    process.stderr.write(`  (cosine baseline skipped via SKIP_COSINE=1)\n\n`);
  } else {
    const errMsg =
      "error" in cosineResult ? cosineResult.error : "no scores returned";
    process.stderr.write(`  ← cosine baseline FAILED: ${errMsg}\n\n`);
  }

  printCrossModelSummary(results, cosineRows);

  const ok = results
    .filter((r) => !r.fatalError && r.calls.some((c) => c.verdict))
    .map((r) => ({ r, knee: bestKnee(r.calls) }))
    .sort((a, b) => b.knee.f1 - a.knee.f1);

  if (ok.length === 0) {
    console.log("All models failed. Check error messages above.");
    process.exit(1);
  }

  const detailed = verbose ? ok : [ok[0]];
  for (const { r } of detailed) printPerModelDetail(r);

  if (!verbose && ok.length > 1) {
    console.log(
      `(detailed report shown for top model only; set VERBOSE=1 for all ${ok.length})`,
    );
  }

  const outPath = process.env.CONTRADICTION_OUTPUT_PATH;
  if (outPath) {
    const fs = await import("node:fs/promises");
    await fs.writeFile(
      outPath,
      JSON.stringify(
        {
          thresholds: CONFIDENCE_THRESHOLDS,
          results: results.map((r) => ({
            model: r.model,
            totalMs: r.totalMs,
            fatalError: r.fatalError,
            calls: r.calls.map((c) => ({
              a: c.pair.a,
              b: c.pair.b,
              bucket: c.pair.bucket,
              gold: c.pair.gold,
              verdict: c.verdict,
              latencyMs: c.latencyMs,
              promptTokens: c.promptTokens,
              completionTokens: c.completionTokens,
              parseError: c.parseError,
            })),
          })),
          cosineBaseline:
            "scores" in cosineResult
              ? cosineResult.scores.map((s) => ({
                  a: s.pair.a,
                  b: s.pair.b,
                  bucket: s.pair.bucket,
                  gold: s.pair.gold,
                  cosine: s.cosine,
                }))
              : null,
        },
        null,
        2,
      ),
    );
    process.stderr.write(`\nRaw dump written to ${outPath}\n`);
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
