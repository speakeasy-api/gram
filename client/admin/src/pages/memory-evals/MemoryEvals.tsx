import { useState, useSyncExternalStore, type JSX } from "react";
import {
  useInfiniteQuery,
  useMutation,
  useQueries,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";

import { useConfirmDialog } from "@/components/ConfirmDialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { errorMessage } from "@/lib/gramAdminApi";

import {
  invalidateMemoryEvals,
  memoryEvalOptionsQuery,
  memoryEvalRunQuery,
  memoryEvalRunsQuery,
  startMemoryEvalRun,
} from "./memoryEvalClient";

import {
  accuracyLabel,
  citationRate,
  comparisonRows,
  isActive,
  memoryWriteCost,
  parseReport,
  runLabel,
  runTiming,
  usd,
  validateConfig,
  type MemoryEvalConfig,
  type MemoryEvalOptions,
  type MemoryEvalReport,
  type MemoryEvalRun,
  type MemoryEvalStatus,
} from "./memoryEvalModel";

const maxCompared = 4;

// Native selects on purpose: the allowed model list is long and plain text,
// and the browser's own picker searches it by typing.
const selectClass =
  "h-9 w-full rounded-md border border-input bg-transparent px-3 text-sm shadow-xs";

const statusVariant: Record<
  MemoryEvalStatus,
  "default" | "secondary" | "destructive" | "outline"
> = {
  queued: "outline",
  running: "secondary",
  succeeded: "default",
  failed: "destructive",
};

export function MemoryEvalsPage(): JSX.Element {
  const client = useQueryClient();
  const options = useQuery(memoryEvalOptionsQuery());
  const runs = useInfiniteQuery({
    ...memoryEvalRunsQuery(),
    // A run can take hours; poll while one is in flight. Active runs
    // are always the newest, so they are on the first page.
    refetchInterval: (query) =>
      query.state.data?.pages[0]?.result.runs.some(isActive) ? 15_000 : false,
  });
  const allRuns = runs.data?.pages.flatMap((page) => page.result.runs);
  const [compared, setCompared] = useState<string[]>([]);
  const mutation = useMutation({
    mutationFn: startMemoryEvalRun,
    onSettled: () => invalidateMemoryEvals(client),
  });

  const active = allRuns?.some(isActive) ?? false;
  const limitReached =
    options.data !== undefined &&
    options.data.runsLast24h >= options.data.dailyRunLimit;

  return (
    <div className="space-y-6 p-6">
      <div>
        <h1 className="text-2xl font-semibold">Organizational Memory evals</h1>
        <p className="text-muted-foreground">
          Score answer, summary and digest models against the synthetic
          Organizational Memory corpus in the demo organization. Runs spend
          model credits and never read customer data.
        </p>
      </div>

      {options.isError && <p role="alert">{errorMessage(options.error)}</p>}
      {options.data && (
        <StartRunForm
          options={options.data}
          disabledReason={
            active
              ? "A run is in progress. Runs share the corpus's memory, so they go one at a time."
              : limitReached
                ? `${options.data.dailyRunLimit} runs started in the last 24 hours.`
                : null
          }
          pending={mutation.isPending}
          error={mutation.isError ? errorMessage(mutation.error) : null}
          onStart={(config) => mutation.mutate(config)}
        />
      )}

      <section className="space-y-2">
        <h2 className="text-lg font-semibold">Runs</h2>
        {runs.isPending && <p>Loading runs…</p>}
        {runs.isError && (
          <div role="alert">
            {errorMessage(runs.error)}{" "}
            <Button onClick={() => void runs.refetch()}>Retry</Button>
          </div>
        )}
        {allRuns?.length === 0 && (
          <p>No runs yet. Start one above to score the default models.</p>
        )}
        {allRuns && allRuns.length > 0 && (
          <RunsTable
            runs={allRuns}
            compared={compared}
            onToggle={(id) =>
              setCompared((current) =>
                current.includes(id)
                  ? current.filter((c) => c !== id)
                  : current.length < maxCompared
                    ? [...current, id]
                    : current,
              )
            }
          />
        )}
        {runs.hasNextPage && (
          <Button
            variant="outline"
            disabled={runs.isFetchingNextPage}
            onClick={() => void runs.fetchNextPage()}
          >
            {runs.isFetchingNextPage ? "Loading…" : "Load more runs"}
          </Button>
        )}
      </section>

      {compared.length > 0 && <Comparison runIds={compared} />}
    </div>
  );
}

function StartRunForm(props: {
  options: MemoryEvalOptions;
  disabledReason: string | null;
  pending: boolean;
  error: string | null;
  onStart: (config: MemoryEvalConfig) => void;
}): JSX.Element {
  const { options } = props;
  const [config, setConfig] = useState<MemoryEvalConfig>(options.defaults);
  const [adding, setAdding] = useState("");
  const [confirm, confirmDialog] = useConfirmDialog();
  const invalid = validateConfig(config, options);

  // A start spends model credit, uses a slot of the shared daily limit and
  // blocks every other start until it finishes, so it is confirmed first.
  const confirmStart = async () => {
    const confirmed = await confirm({
      title: "Start a memory eval run?",
      description: `${config.answerModels.join(", ")} × ${config.runs}, judged by ${config.judgeModel}, with ${config.summaryModel} summaries and ${config.digestModel} digests. It spends model credit, uses 1 of ${options.dailyRunLimit} runs for the next 24 hours, and no other run can start until it finishes.`,
      confirmLabel: "Start run",
    });
    if (confirmed) props.onStart(config);
  };

  const modelSelect = (
    label: string,
    key: "judgeModel" | "summaryModel" | "digestModel",
  ) => (
    <label className="block space-y-1 text-sm">
      {label}
      <select
        className={selectClass}
        value={config[key]}
        onChange={(event) =>
          setConfig({ ...config, [key]: event.target.value })
        }
      >
        {options.allowedModels.map((m) => (
          <option key={m} value={m}>
            {m}
          </option>
        ))}
      </select>
    </label>
  );

  return (
    <form
      className="space-y-4 rounded-lg border p-4"
      onSubmit={(event) => {
        event.preventDefault();
        if (!invalid && !props.disabledReason && !props.pending)
          void confirmStart();
      }}
    >
      {confirmDialog}
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold">Start a run</h2>
        <span className="text-sm text-muted-foreground">
          {options.runsLast24h} of {options.dailyRunLimit} runs used in the last
          24 hours
        </span>
      </div>

      <div className="space-y-2">
        <span className="text-sm">
          Answer models (up to {options.maxAnswerModels})
        </span>
        <div className="flex flex-wrap gap-2">
          {config.answerModels.map((m) => (
            <Badge key={m} variant="secondary" className="gap-1">
              {m}
              <button
                type="button"
                aria-label={`Remove ${m}`}
                onClick={() =>
                  setConfig({
                    ...config,
                    answerModels: config.answerModels.filter((x) => x !== m),
                  })
                }
              >
                ×
              </button>
            </Badge>
          ))}
        </div>
        <div className="flex gap-2">
          <select
            aria-label="Add answer model"
            className={selectClass}
            value={adding}
            onChange={(event) => setAdding(event.target.value)}
          >
            <option value="">Choose a model…</option>
            {options.allowedModels
              .filter((m) => !config.answerModels.includes(m))
              .map((m) => (
                <option key={m} value={m}>
                  {m}
                </option>
              ))}
          </select>
          <Button
            type="button"
            variant="outline"
            disabled={
              !adding ||
              config.answerModels.includes(adding) ||
              config.answerModels.length >= options.maxAnswerModels
            }
            onClick={() => {
              setConfig({
                ...config,
                answerModels: [...config.answerModels, adding],
              });
              setAdding("");
            }}
          >
            Add
          </Button>
        </div>
      </div>

      <div className="grid gap-4 md:grid-cols-4">
        {modelSelect("Judge model", "judgeModel")}
        {modelSelect("Summary model", "summaryModel")}
        {modelSelect("Digest model", "digestModel")}
        <label className="block space-y-1 text-sm">
          Repeats per model and arm
          <Input
            type="number"
            min={1}
            max={options.maxRuns}
            value={config.runs}
            onChange={(event) =>
              setConfig({ ...config, runs: Number(event.target.value) })
            }
          />
        </label>
      </div>

      {invalid && <p className="text-sm text-destructive">{invalid}</p>}
      {props.disabledReason && (
        <p className="text-sm text-muted-foreground">{props.disabledReason}</p>
      )}
      {props.error && <p role="alert">{props.error}</p>}
      <div className="flex gap-2">
        <Button
          type="submit"
          disabled={
            props.pending || invalid !== null || props.disabledReason !== null
          }
        >
          {props.pending ? "Starting…" : "Start run"}
        </Button>
        <Button
          type="button"
          variant="outline"
          onClick={() => {
            setConfig(options.defaults);
            setAdding("");
          }}
        >
          Reset to defaults
        </Button>
      </div>
    </form>
  );
}

function subscribeToMinutes(onTick: () => void): () => void {
  const id = setInterval(onTick, 60_000);
  return () => clearInterval(id);
}

// Floored to the minute so the snapshot is stable between renders.
function currentMinute(): number {
  return Math.floor(Date.now() / 60_000) * 60_000;
}

/** useNow ticks once a minute so elapsed times stay current. */
function useNow(): number {
  return useSyncExternalStore(subscribeToMinutes, currentMinute);
}

function RunsTable(props: {
  runs: MemoryEvalRun[];
  compared: string[];
  onToggle: (id: string) => void;
}): JSX.Element {
  const now = useNow();
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Compare</TableHead>
          <TableHead>Queued (UTC)</TableHead>
          <TableHead>Status</TableHead>
          <TableHead>Answer models</TableHead>
          <TableHead>Summary / digest</TableHead>
          <TableHead>Cost</TableHead>
          <TableHead>By</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {props.runs.map((run) => (
          <TableRow key={run.id}>
            <TableCell>
              <Checkbox
                aria-label={`Compare run ${runLabel(run)}`}
                disabled={
                  run.status !== "succeeded" ||
                  (!props.compared.includes(run.id) &&
                    props.compared.length >= maxCompared)
                }
                checked={props.compared.includes(run.id)}
                onCheckedChange={() => props.onToggle(run.id)}
              />
            </TableCell>
            <TableCell className="font-mono text-xs">
              {runLabel(run)}
              <br />
              <span className="text-muted-foreground">
                {runTiming(run, now)}
              </span>
            </TableCell>
            <TableCell>
              <Badge variant={statusVariant[run.status]}>{run.status}</Badge>
              {run.error && (
                <p className="mt-1 max-w-xs text-xs whitespace-normal text-destructive">
                  {run.error}
                </p>
              )}
            </TableCell>
            <TableCell className="max-w-64 text-xs whitespace-normal">
              {run.config.answerModels.join(", ")}
              <br />
              <span className="text-muted-foreground">
                ×{run.config.runs}, judged by {run.config.judgeModel}
              </span>
            </TableCell>
            <TableCell className="max-w-64 text-xs whitespace-normal">
              {run.config.summaryModel}
              <br />
              {run.config.digestModel}
            </TableCell>
            <TableCell>{usd(run.costUsd)}</TableCell>
            <TableCell className="max-w-64 text-xs whitespace-normal">
              {run.requestedBy}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

function Comparison(props: { runIds: string[] }): JSX.Element {
  const results = useQueries({
    queries: props.runIds.map((id) => memoryEvalRunQuery(id)),
  });
  const failed = results.find((r) => r.isError);
  const loaded: { run: MemoryEvalRun; report: MemoryEvalReport }[] = [];
  for (const r of results) {
    const report = r.data ? parseReport(r.data) : null;
    if (r.data && report) loaded.push({ run: r.data, report });
  }
  const rows = comparisonRows(loaded);

  return (
    <section className="space-y-2">
      <h2 className="text-lg font-semibold">Comparison</h2>
      <p className="text-sm text-muted-foreground">
        Accuracy is correct over graded answers, so compare runs graded by the
        same judge. The best accuracy per arm among rows without leaks is bold.
        A red row leaked a planted secret or another project's canary, which
        disqualifies it whatever its accuracy. A dash means nothing in that arm
        was graded. A cost marked ≥ is a lower bound: some calls reported no
        price. Times are UTC.
      </p>
      {failed?.error && <p role="alert">{errorMessage(failed.error)}</p>}
      {results.some((r) => r.isPending) && <p>Loading reports…</p>}
      {loaded.map(({ run, report }) =>
        report.preparation ? (
          <p key={run.id} className="text-xs text-muted-foreground">
            {runLabel(run)}: {report.preparation.summaries} summaries (
            {report.preparation.failed_summaries} failed) and{" "}
            {report.preparation.digests} digests, memory written for{" "}
            {memoryWriteCost(report.preparation)}; grading{" "}
            {usd(report.judge_cost_usd)}.
          </p>
        ) : null,
      )}
      {rows.length > 0 && (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Arm</TableHead>
              <TableHead>Answer model</TableHead>
              <TableHead>Summary / digest</TableHead>
              <TableHead>Judge</TableHead>
              <TableHead>Run (UTC)</TableHead>
              <TableHead>Accuracy</TableHead>
              <TableHead>Valid citations</TableHead>
              <TableHead>Leaks</TableHead>
              <TableHead>Errors</TableHead>
              <TableHead>Cost</TableHead>
              <TableHead>p95 latency</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((row) => (
              <TableRow
                key={`${row.runId}-${row.score.model}-${row.score.arm}`}
                className={row.unsafe ? "bg-destructive/10" : undefined}
              >
                <TableCell>{row.score.arm}</TableCell>
                <TableCell className="max-w-64 text-xs whitespace-normal">
                  {row.score.model}
                </TableCell>
                <TableCell className="max-w-64 text-xs whitespace-normal">
                  {row.score.has_tools ? (
                    <>
                      {row.summaryModel}
                      <br />
                      {row.digestModel}
                    </>
                  ) : (
                    "—"
                  )}
                </TableCell>
                <TableCell className="max-w-48 text-xs whitespace-normal">
                  {row.judgeModel}
                </TableCell>
                <TableCell className="font-mono text-xs">
                  {row.runLabel}
                </TableCell>
                <TableCell className={row.best ? "font-semibold" : undefined}>
                  {accuracyLabel(row.score)}
                </TableCell>
                <TableCell>{citationRate(row.score)}</TableCell>
                <TableCell>
                  {(row.score.leaks?.length ?? 0) +
                    (row.score.isolation_violations?.length ?? 0)}
                </TableCell>
                <TableCell>{row.score.errors}</TableCell>
                <TableCell
                  title={`${row.score.prompt_tokens.toLocaleString()} tokens in, ${row.score.completion_tokens.toLocaleString()} out`}
                >
                  {usd(row.score.cost_usd)}
                </TableCell>
                <TableCell>{row.score.latency_p95_ms} ms</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </section>
  );
}
