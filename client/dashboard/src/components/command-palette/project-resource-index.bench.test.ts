import { gzipSync } from "node:zlib";
import { performance } from "node:perf_hooks";
import { describe, expect, test } from "vitest";
import type { LauncherCandidate } from "./candidates/types";
import { prefilter, rank } from "./ranker";

type WireItem = {
  kind:
    | "assistant"
    | "environment"
    | "gateway"
    | "mcp"
    | "plugin"
    | "rule"
    | "skill";
  id: string;
  name: string;
  route_selector: string;
  keywords?: string[];
};
type WireIndex = {
  schema_version: string;
  generated_at: string;
  items: WireItem[];
  kinds: Array<{
    kind: string;
    complete: boolean;
    overflow: boolean;
    item_count: number;
  }>;
};

const kinds = [
  "assistant",
  "environment",
  "gateway",
  "mcp",
  "plugin",
  "rule",
  "skill",
] as const;

function payload(size: number): string {
  const counts = new Map<string, number>();
  const items: WireItem[] = Array.from({ length: size }, (_, i) => {
    const kind = kinds[i % kinds.length]!;
    counts.set(kind, (counts.get(kind) ?? 0) + 1);
    const id = `00000000-0000-4000-8000-${(i + 1).toString(16).padStart(12, "0")}`;
    const slug = `resource-${i.toString().padStart(6, "0")}`;
    return {
      kind,
      id,
      name: `Resource ${i.toString().padStart(6, "0")}`,
      route_selector:
        kind === "assistant" || kind === "gateway" || kind === "plugin"
          ? id
          : slug,
      ...(kind === "assistant" || kind === "gateway"
        ? {}
        : { keywords: [`Resource ${i.toString().padStart(6, "0")}`, slug] }),
    };
  });
  return JSON.stringify({
    schema_version: "1",
    generated_at: "2026-01-01T00:00:00Z",
    items,
    kinds: kinds.map((kind) => ({
      kind,
      complete: true,
      overflow: false,
      item_count: counts.get(kind) ?? 0,
    })),
  } satisfies WireIndex);
}

function mapCandidates(index: WireIndex): LauncherCandidate[] {
  return index.items.map((item) => {
    const selector = item.route_selector;
    return {
      id: `${item.kind}:${item.id}`,
      kind: item.kind === "mcp" ? "mcp_server" : item.kind,
      title: item.name,
      detail: item.kind,
      keywords: [
        ...new Set([
          item.kind,
          selector,
          item.id,
          ...(item.keywords ?? []).slice(0, 32),
        ]),
      ],
      resourceIdentity: { id: item.id, routeSelector: selector },
      verbs: ["open"],
      group: item.kind,
      run: () => undefined,
    } satisfies LauncherCandidate;
  });
}

function median(values: number[]): number {
  return [...values].sort((a, b) => a - b)[Math.floor(values.length / 2)]!;
}

describe("project resource index synthetic performance evidence", () => {
  test.each([1_000, 10_000, 50_000])(
    "%i items",
    (size) => {
      const json = payload(size);
      const parseMapMs: number[] = [];
      const fuzzyRankMs: number[] = [];
      const memoryDeltas: number[] = [];
      let candidates: LauncherCandidate[] = [];
      for (let iteration = 0; iteration < 7; iteration++) {
        const beforeHeap = process.memoryUsage().heapUsed;
        const started = performance.now();
        candidates = mapCandidates(JSON.parse(json) as WireIndex);
        parseMapMs.push(performance.now() - started);
        memoryDeltas.push(
          Math.max(0, process.memoryUsage().heapUsed - beforeHeap),
        );

        const rankStarted = performance.now();
        const ranked = rank(prefilter("resource 049", candidates));
        fuzzyRankMs.push(performance.now() - rankStarted);
        expect(ranked.length).toBeGreaterThan(0);
      }
      expect(candidates).toHaveLength(size);
      console.log(
        [
          "PROJECT_RESOURCE_INDEX_CLIENT",
          `items=${size}`,
          `json_bytes=${Buffer.byteLength(json)}`,
          `gzip_bytes=${gzipSync(json).byteLength}`,
          `parse_map_ms=${median(parseMapMs).toFixed(3)}`,
          `heap_delta_bytes_approx=${median(memoryDeltas).toFixed(0)}`,
          `fuzzy_prefilter_rank_ms=${median(fuzzyRankMs).toFixed(3)}`,
        ].join("\t"),
      );
    },
    30_000,
  );
});
