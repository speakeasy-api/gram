import { useState, type JSX } from "react";
import {
  useMutation,
  useQueries,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";

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
import {
  errorMessage,
  gramAdminFetch,
  gramAdminMutation,
} from "@/lib/gramAdminApi";

import {
  citationRate,
  comparisonRows,
  isActive,
  parseReport,
  percent,
  runLabel,
  usd,
  validateConfig,
  type MemoryEvalConfig,
  type MemoryEvalOptions,
  type MemoryEvalReport,
  type MemoryEvalRun,
  type MemoryEvalStatus,
} from "./memoryEvalModel";

const optionsKey = ["admin", "memory-evals", "options"];
const runsKey = ["admin", "memory-evals", "runs"];
const runKey = (id: string) => ["admin", "memory-evals", "run", id];
const maxCompared = 4;

const selectClass =
  "h-9 w-full rounded-md border border-input bg-transparent px-3 text-sm shadow-xs";

function startRun(config: MemoryEvalConfig) {
  return gramAdminMutation<MemoryEvalRun>("/admin/memoryEval.start", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ config }),
  });
}

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
  const options = useQuery({
    queryKey: optionsKey,
    queryFn: () =>
      gramAdminFetch<MemoryEvalOptions>("/admin/memoryEval.options"),
  });
  const runs = useQuery({
    queryKey: runsKey,
    queryFn: () =>
      gramAdminFetch<{ runs: MemoryEvalRun[]; next_cursor?: string }>(
        "/admin/memoryEval.runs?limit=50",
      ),
    // A run takes up to an hour; poll while one is in flight.
    refetchInterval: (query) =>
      query.state.data?.runs.some(isActive) ? 15_000 : false,
  });
  const [compared, setCompared] = useState<string[]>([]);
  const mutation = useMutation({
    mutationFn: startRun,
    onSuccess: async () => {
      await Promise.all([
        client.invalidateQueries({ queryKey: runsKey }),
        client.invalidateQueries({ queryKey: optionsKey }),
      ]);
    },
  });

  const active = runs.data?.runs.some(isActive) ?? false;
  const limitReached =
    options.data !== undefined &&
    options.data.runs_today >= options.data.daily_run_limit;

  return (
    <div className="space-y-6 p-6">
      <div>
        <h1 className="text-2xl font-semibold">Team memory evals</h1>
        <p className="text-muted-foreground">
          Score answer, summary and digest models against the synthetic team
          memory corpus in the demo organization. Runs spend model credits and
          never read customer data.
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
                ? `${options.data.daily_run_limit} runs started in the last 24 hours.`
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
        {runs.data?.runs.length === 0 && (
          <p>No runs yet. Start one above to score the default models.</p>
        )}
        {runs.data && runs.data.runs.length > 0 && (
          <RunsTable
            runs={runs.data.runs}
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
  const invalid = validateConfig(config, options.allowed_models);

  const modelSelect = (
    label: string,
    key: "judge_model" | "summary_model" | "digest_model",
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
        {options.allowed_models.map((m) => (
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
        if (!invalid && !props.disabledReason) props.onStart(config);
      }}
    >
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold">Start a run</h2>
        <span className="text-sm text-muted-foreground">
          {options.runs_today} of {options.daily_run_limit} runs used in the
          last 24 hours
        </span>
      </div>

      <div className="space-y-2">
        <span className="text-sm">Answer models (up to 4)</span>
        <div className="flex flex-wrap gap-2">
          {config.answer_models.map((m) => (
            <Badge key={m} variant="secondary" className="gap-1">
              {m}
              <button
                type="button"
                aria-label={`Remove ${m}`}
                onClick={() =>
                  setConfig({
                    ...config,
                    answer_models: config.answer_models.filter((x) => x !== m),
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
            {options.allowed_models
              .filter((m) => !config.answer_models.includes(m))
              .map((m) => (
                <option key={m} value={m}>
                  {m}
                </option>
              ))}
          </select>
          <Button
            type="button"
            variant="outline"
            disabled={!adding || config.answer_models.length >= 4}
            onClick={() => {
              setConfig({
                ...config,
                answer_models: [...config.answer_models, adding],
              });
              setAdding("");
            }}
          >
            Add
          </Button>
        </div>
      </div>

      <div className="grid gap-4 md:grid-cols-4">
        {modelSelect("Judge model", "judge_model")}
        {modelSelect("Summary model", "summary_model")}
        {modelSelect("Digest model", "digest_model")}
        <label className="block space-y-1 text-sm">
          Repeats per model and arm
          <Input
            type="number"
            min={1}
            max={3}
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
          onClick={() => setConfig(options.defaults)}
        >
          Reset to defaults
        </Button>
      </div>
    </form>
  );
}

function RunsTable(props: {
  runs: MemoryEvalRun[];
  compared: string[];
  onToggle: (id: string) => void;
}): JSX.Element {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Compare</TableHead>
          <TableHead>Started</TableHead>
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
                disabled={run.status !== "succeeded"}
                checked={props.compared.includes(run.id)}
                onCheckedChange={() => props.onToggle(run.id)}
              />
            </TableCell>
            <TableCell className="font-mono text-xs">{runLabel(run)}</TableCell>
            <TableCell>
              <Badge variant={statusVariant[run.status]}>{run.status}</Badge>
              {run.error && (
                <p className="mt-1 max-w-xs text-xs text-destructive">
                  {run.error}
                </p>
              )}
            </TableCell>
            <TableCell className="text-xs">
              {run.config.answer_models.join(", ")}
              <span className="text-muted-foreground">
                {" "}
                · ×{run.config.runs}, judged by {run.config.judge_model}
              </span>
            </TableCell>
            <TableCell className="text-xs">
              {run.config.summary_model}
              <br />
              {run.config.digest_model}
            </TableCell>
            <TableCell>{usd(run.cost_usd)}</TableCell>
            <TableCell className="text-xs">{run.requested_by}</TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

function Comparison(props: { runIds: string[] }): JSX.Element {
  const results = useQueries({
    queries: props.runIds.map((id) => ({
      queryKey: runKey(id),
      queryFn: () =>
        gramAdminFetch<MemoryEvalRun>(
          `/admin/memoryEval.run?id=${encodeURIComponent(id)}`,
        ),
      staleTime: Infinity,
    })),
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
        Accuracy is correct over graded answers. The best accuracy per arm is
        bold. A red row leaked a planted secret or another project's canary,
        which disqualifies it whatever its accuracy.
      </p>
      {failed?.error && <p role="alert">{errorMessage(failed.error)}</p>}
      {results.some((r) => r.isPending) && <p>Loading reports…</p>}
      {loaded.map(({ run, report }) =>
        report.preparation ? (
          <p key={run.id} className="text-xs text-muted-foreground">
            {runLabel(run)}: {report.preparation.summaries} summaries (
            {report.preparation.failed_summaries} failed) and{" "}
            {report.preparation.digests} digests, memory written for{" "}
            {usd(
              report.preparation.summary_spend.cost_usd +
                report.preparation.digest_spend.cost_usd,
            )}
            ; grading {usd(report.judge_cost_usd)}.
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
              <TableHead>Run</TableHead>
              <TableHead>Accuracy</TableHead>
              <TableHead>Valid citations</TableHead>
              <TableHead>Leaks</TableHead>
              <TableHead>Errors</TableHead>
              <TableHead>Tokens in / out</TableHead>
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
                <TableCell className="text-xs">{row.score.model}</TableCell>
                <TableCell className="text-xs">
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
                <TableCell className="font-mono text-xs">
                  {row.runLabel}
                </TableCell>
                <TableCell className={row.best ? "font-semibold" : undefined}>
                  {percent(row.score.accuracy)}
                </TableCell>
                <TableCell>{citationRate(row.score)}</TableCell>
                <TableCell>
                  {(row.score.leaks?.length ?? 0) +
                    (row.score.isolation_violations?.length ?? 0)}
                </TableCell>
                <TableCell>{row.score.errors}</TableCell>
                <TableCell className="text-xs">
                  {row.score.prompt_tokens.toLocaleString()} /{" "}
                  {row.score.completion_tokens.toLocaleString()}
                </TableCell>
                <TableCell>{usd(row.score.cost_usd)}</TableCell>
                <TableCell>{row.score.latency_p95_ms} ms</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </section>
  );
}
