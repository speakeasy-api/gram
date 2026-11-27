import { useEffect, useId, useMemo, useRef, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { z } from "zod";
import { Button } from "@/components/ui/Button";
import { CodeSnippet } from "@/components/ui/CodeSnippet";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { StatusDot } from "@/components/ui/StatusDot";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import type { InspectTool } from "../inspect/types";
import {
  extractFields,
  jsonFieldError,
  toArguments,
} from "../inspect/toolSchemaFields";
import { FieldInput } from "../inspect/ToolRunPanel";
import {
  codeModeCallSnippet,
  DEFAULT_CODE_MODE_SOURCE,
} from "./codeModeSnippet";
import { PythonEditor } from "./PythonEditor";
import { GatewayRPCError, gatewayRPC } from "./useGatewayInspection";

const resultSchema = z.object({
  value: z.unknown(),
  output: z.string(),
  output_truncated: z.boolean(),
  error: z.object({ code: z.string(), message: z.string() }).optional(),
  tool_calls: z.array(z.object({ path: z.string(), outcome: z.string() })),
});

/** A run refused before its program was submitted, such as at capacity. */
const refusalSchema = z.object({
  error: z.object({
    code: z.string(),
    message: z.string(),
    retryable: z.boolean(),
  }),
});

/** The gateway's own time for an execute request, from the result's _meta. */
const timingSchema = z.object({
  server_ms: z.number(),
  admission_ms: z.number().optional(),
  run_ms: z.number().optional(),
});
type ServerTiming = z.infer<typeof timingSchema>;

const TIMING_META_KEY = "com.speakeasy.ai/timing";

class ExecutionNotAdmittedError extends Error {
  readonly timing: ServerTiming | undefined;

  constructor(message: string, timing?: ServerTiming) {
    super(message);
    this.timing = timing;
  }
}

const JSONRPC_INTERNAL_ERROR = -32603;

/**
 * The gateway rejected the call before submitting the program. An internal
 * error is not treated as a refusal: the gateway also reports failures that
 * follow execution that way, so its outcome stays unknown.
 */
function refusedBeforeExecution(error: unknown): error is GatewayRPCError {
  if (!(error instanceof GatewayRPCError)) return false;
  if (error.code !== undefined) return error.code !== JSONRPC_INTERNAL_ERROR;
  return error.status >= 400 && error.status < 500;
}

/** Executes only on explicit submission; transport failures never replay code. */
export function GatewayCodeExecution({
  connectUrl,
  headers,
  tool,
  disabled = false,
}: {
  disabled?: boolean;
  connectUrl: string;
  headers: Record<string, string> | undefined;
  /**
   * The member tool picked in the list. Its arguments get a form, and the
   * editor holds a call to it built from that form.
   */
  tool?: InspectTool;
}): JSX.Element {
  const hintId = useId();
  const [code, setCode] = useState(DEFAULT_CODE_MODE_SOURCE);
  // The text this panel last wrote itself. While the editor still holds it,
  // the user hasn't typed, so the form (or a newly picked tool) rewrites it.
  const [generated, setGenerated] = useState(DEFAULT_CODE_MODE_SOURCE);
  const following = code === generated;
  // Form values per tool, so switching tools keeps what was typed.
  const [valuesByTool, setValuesByTool] = useState<
    Record<string, Record<string, string>>
  >({});
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const fields = useMemo(
    () =>
      // Required arguments first; the rest keep the schema's order.
      extractFields(tool?.inputSchema).sort(
        (a, b) => Number(b.required) - Number(a.required),
      ),
    [tool?.inputSchema],
  );
  const visibleFields = fields.filter((field) => field.kind !== "const");
  // Null-prototype copy: schema field names such as `constructor` must read
  // as unset, never as an inherited Object member.
  const values: Record<string, string> = Object.assign(
    Object.create(null) as Record<string, string>,
    tool && Object.hasOwn(valuesByTool, tool.name)
      ? valuesByTool[tool.name]
      : undefined,
  );
  // Errors are per tool and field, so another tool's field never shows them.
  const errorKey = (fieldName: string) =>
    JSON.stringify([tool?.name ?? "", fieldName]);
  const errorFor = (fieldName: string) =>
    Object.hasOwn(fieldErrors, errorKey(fieldName))
      ? fieldErrors[errorKey(fieldName)]
      : undefined;
  const snippet = tool
    ? codeModeCallSnippet(tool.name, fields, values)
    : undefined;
  const [offered, setOffered] = useState<string>();
  if (snippet !== undefined && snippet !== offered) {
    setOffered(snippet);
    if (following) {
      setCode(snippet);
      setGenerated(snippet);
    }
  }
  const [ranFor, setRanFor] = useState<string>();
  const regenerate = () => {
    if (snippet === undefined) return;
    setCode(snippet);
    setGenerated(snippet);
  };
  // While the editor follows the form, the form must be complete to run.
  const formCheck = tool && following ? toArguments(fields, values) : undefined;
  const incomplete = formCheck && !formCheck.ok ? formCheck.error : undefined;
  const request = useRef<(() => void) | undefined>(undefined);
  const startedAt = useRef<number | undefined>(undefined);
  const [durationMs, setDurationMs] = useState<number>();
  useEffect(() => () => request.current?.(), []);
  const execution = useMutation({
    mutationFn: async (source: string) => {
      const controller = new AbortController();
      const requestId = crypto.randomUUID();
      const cancel = () => {
        if (controller.signal.aborted) return;
        // Capture the submitted credential and ID: refreshed tokens cannot cancel it.
        void fetch(connectUrl, {
          method: "POST",
          keepalive: true,
          headers: {
            "Content-Type": "application/json",
            Accept: "application/json, text/event-stream",
            ...headers,
          },
          body: JSON.stringify({
            jsonrpc: "2.0",
            method: "notifications/cancelled",
            params: { requestId },
          }),
          signal: AbortSignal.timeout(2000),
        }).catch(() => undefined);
        controller.abort();
      };
      request.current = cancel;
      try {
        const raw = await gatewayRPC(
          connectUrl,
          headers,
          "tools/call",
          { name: "execute", arguments: { code: source } },
          controller.signal,
          requestId,
        );
        const response = z
          .object({
            structuredContent: z.unknown().optional(),
            content: z
              .array(z.object({ text: z.string().optional() }).passthrough())
              .optional(),
            isError: z.boolean().optional(),
            _meta: z
              .object({ [TIMING_META_KEY]: timingSchema.optional() })
              .passthrough()
              .optional(),
          })
          .parse(raw);
        const timing = response._meta?.[TIMING_META_KEY];
        if (!response.structuredContent) {
          const message =
            response.content?.[0]?.text ??
            "The gateway returned no execution result.";
          if (response.isError)
            throw new ExecutionNotAdmittedError(message, timing);
          throw new Error(message);
        }
        const refusal = refusalSchema.safeParse(response.structuredContent);
        if (response.isError && refusal.success) {
          throw new ExecutionNotAdmittedError(
            refusal.data.error.message,
            timing,
          );
        }
        return {
          result: resultSchema.parse(response.structuredContent),
          timing,
        };
      } catch (error) {
        if (error instanceof ExecutionNotAdmittedError) throw error;
        if (refusedBeforeExecution(error)) {
          throw new ExecutionNotAdmittedError(error.message);
        }
        if (controller.signal.aborted) {
          throw new Error(
            "Cancellation requested. Tools already dispatched may have completed; check their state before running again.",
          );
        }
        const detail =
          error instanceof Error
            ? error.message
            : "The response could not be read.";
        throw new Error(
          `${detail} Execution outcome is unknown; tools may have run. Check their state before running again.`,
        );
      } finally {
        if (request.current === cancel) request.current = undefined;
      }
    },
    onSettled: () => {
      if (startedAt.current !== undefined) {
        setDurationMs(performance.now() - startedAt.current);
      }
    },
    retry: false,
    throwOnError: false,
  });
  const sourceBytes = new TextEncoder().encode(code).length;
  const tooLarge = sourceBytes > MAX_SOURCE_BYTES;
  // A run belongs to the tool picked when it started, like the Run tab's
  // per-tool results: picking another tool hides it until that one runs.
  // No member tool picked is its own owner (""), so a member tool's run never
  // shows under a gateway-owned tool or under nothing.
  const showRun = ranFor !== undefined && ranFor === (tool?.name ?? "");
  const result = showRun ? execution.data?.result : undefined;
  const submitted = execution.variables;
  const canRun =
    !disabled &&
    !execution.isPending &&
    !!code.trim() &&
    !tooLarge &&
    !incomplete;
  const run = () => {
    if (canRun) {
      startedAt.current = performance.now();
      setRanFor(tool?.name ?? "");
      execution.mutate(code);
    }
  };

  return (
    <section aria-label="Run Python" className="flex flex-col gap-5">
      <form
        onSubmit={(event) => {
          event.preventDefault();
          run();
        }}
        className="flex flex-col gap-5"
      >
        {tool ? (
          <div className="flex flex-col gap-4">
            <div className="flex flex-col gap-1.5">
              <span className="min-w-0 font-mono text-sm font-medium break-all">
                {tool.name}
              </span>
              {tool.description ? (
                <p className="text-muted-foreground line-clamp-4 text-[13px] leading-5 whitespace-pre-line">
                  {tool.description}
                </p>
              ) : null}
            </div>
            <fieldset className="border-border flex flex-col gap-4 border-t pt-5">
              <legend className="sr-only">Arguments</legend>
              <span aria-hidden className="text-eyebrow">
                Arguments
              </span>
              {visibleFields.length === 0 ? (
                <p className="text-muted-foreground text-[13px]">
                  This tool takes no arguments.
                </p>
              ) : (
                visibleFields.map((field) => (
                  <FieldInput
                    key={`${tool.name}:${field.name}`}
                    field={field}
                    value={values[field.name] ?? ""}
                    error={errorFor(field.name)}
                    disabled={execution.isPending}
                    onChange={(value) => {
                      setValuesByTool((current) => ({
                        ...current,
                        [tool.name]: {
                          ...(Object.hasOwn(current, tool.name)
                            ? current[tool.name]
                            : undefined),
                          [field.name]: value,
                        },
                      }));
                      if (errorFor(field.name))
                        setFieldErrors(
                          ({ [errorKey(field.name)]: _, ...rest }) => rest,
                        );
                    }}
                    onBlur={(value) => {
                      const error = jsonFieldError(field, value);
                      setFieldErrors(({ [errorKey(field.name)]: _, ...rest }) =>
                        error
                          ? { ...rest, [errorKey(field.name)]: error }
                          : rest,
                      );
                    }}
                  />
                ))
              )}
            </fieldset>
          </div>
        ) : null}
        <div className="flex flex-col gap-3">
          <PythonEditor
            label="Python source"
            actions={
              tool && !following ? (
                <button
                  type="button"
                  onClick={regenerate}
                  disabled={execution.isPending}
                  className="text-muted-fixed-light hover:text-default-fixed-light min-w-0 truncate text-xs underline underline-offset-2"
                >
                  Regenerate from arguments
                </button>
              ) : undefined
            }
            value={code}
            onChange={setCode}
            onSubmit={run}
            disabled={execution.isPending}
            describedBy={tooLarge || incomplete ? hintId : undefined}
          />
          <div className="flex items-center justify-end gap-3">
            {tooLarge ? (
              <p
                id={hintId}
                role="alert"
                className="text-destructive mr-auto text-xs"
              >
                {`Source is ${formatBytes(sourceBytes)}; the limit is 64 KB.`}
              </p>
            ) : incomplete ? (
              <p id={hintId} className="text-muted-foreground mr-auto text-xs">
                {incomplete}
              </p>
            ) : null}
            <div className="flex shrink-0 items-center gap-2">
              {execution.isPending && (
                <Button
                  type="button"
                  size="sm"
                  variant="secondary"
                  className="font-sans text-[13px] font-medium tracking-normal normal-case"
                  onClick={() => request.current?.()}
                >
                  <Button.Text>Cancel</Button.Text>
                </Button>
              )}
              <Button
                type="submit"
                size="sm"
                className="font-sans text-[13px] font-medium tracking-normal normal-case"
                disabled={!canRun}
                title="Execute code (Ctrl/⌘ Enter)"
                aria-keyshortcuts="Control+Enter Meta+Enter"
              >
                <Button.Text>
                  {execution.isPending ? "Executing…" : "Execute code"}
                </Button.Text>
              </Button>
            </div>
          </div>
        </div>
      </form>

      {showRun && execution.isError && (
        <Alert variant="error" dismissible={false}>
          <AlertTitle>
            {execution.error instanceof ExecutionNotAdmittedError
              ? "The run was refused"
              : "The run did not finish"}
          </AlertTitle>
          <AlertDescription>
            {execution.error.message}
            {execution.error instanceof ExecutionNotAdmittedError &&
              execution.error.timing && (
                <span className="text-muted-foreground block text-xs">
                  {`Refused after ${formatDuration(execution.error.timing.server_ms)} on the server.`}
                </span>
              )}
          </AlertDescription>
        </Alert>
      )}

      {result && submitted !== undefined && (
        <ExecutionResult
          result={result}
          durationMs={durationMs}
          serverTiming={execution.data?.timing}
          submitted={submitted !== code ? submitted : undefined}
        />
      )}
    </section>
  );
}

const MAX_SOURCE_BYTES = 64 * 1024;

function formatBytes(bytes: number): string {
  return `${(bytes / 1024).toFixed(1)} KB`;
}

const outcomeLabels: Record<
  string,
  { label: string; variant: "success" | "warning" | "neutral" }
> = {
  completed: { label: "Completed", variant: "success" },
  not_dispatched: { label: "Not run", variant: "neutral" },
  unknown: { label: "Outcome unknown", variant: "warning" },
};

function ExecutionResult({
  result,
  durationMs,
  serverTiming,
  submitted,
}: {
  result: z.infer<typeof resultSchema>;
  /** Round trip measured in the browser, including the network. */
  durationMs: number | undefined;
  serverTiming: ServerTiming | undefined;
  /** The source that produced this result, when the editor has moved on. */
  submitted: string | undefined;
}): JSX.Element {
  const calls = result.tool_calls;
  const details = [
    `${calls.length} tool ${calls.length === 1 ? "call" : "calls"}`,
    serverTiming && `${formatDuration(serverTiming.server_ms)} on the server`,
    serverTiming?.admission_ms !== undefined &&
    serverTiming.run_ms !== undefined
      ? `${formatDuration(serverTiming.admission_ms)} waiting for capacity, ${formatDuration(serverTiming.run_ms)} running`
      : undefined,
  ]
    .filter(Boolean)
    .join(" · ");
  const unknownOutcomes = calls.some((call) => call.outcome === "unknown");

  return (
    <section
      aria-label="Response"
      aria-live="polite"
      className="border-border flex flex-col gap-3 border-t pt-5"
    >
      <div className="flex flex-col gap-1">
        <div className="flex items-center justify-between gap-2">
          <span className="text-eyebrow">Response</span>
          <span className="flex items-center gap-2 text-xs">
            <StatusDot tone={result.error ? "destructive" : "success"} />
            <span className={cn(result.error && "text-destructive")}>
              {result.error ? "Failed" : "Returned"}
            </span>
            {durationMs !== undefined ? (
              <span className="text-muted-foreground font-mono tabular-nums">
                {formatDuration(durationMs)}
              </span>
            ) : null}
          </span>
        </div>
        <p className="text-muted-foreground text-xs">{details}</p>
      </div>
      {result.error && (
        <Alert variant="error" dismissible={false}>
          <AlertTitle>{result.error.message}</AlertTitle>
          <AlertDescription>
            <span className="font-mono text-xs">{result.error.code}</span>
          </AlertDescription>
        </Alert>
      )}
      {unknownOutcomes && (
        <Alert variant="warning" dismissible={false}>
          <AlertTitle>Some tool outcomes are unknown</AlertTitle>
          <AlertDescription>
            Those tools may have run. Check their state before running again.
          </AlertDescription>
        </Alert>
      )}

      <ResultBlock title="Return value">
        <CodeSnippet
          language="json"
          code={JSON.stringify(result.value ?? null, null, 2)}
          fontSize="small"
          copyable
          wordWrap
          className="max-h-[28rem] overflow-auto border-0"
        />
      </ResultBlock>

      {(result.output || result.output_truncated) && (
        <ResultBlock
          title="Printed output"
          meta={result.output_truncated ? "Truncated" : undefined}
        >
          <pre className="max-h-64 overflow-auto px-3 py-2.5 font-mono text-xs leading-relaxed break-words whitespace-pre-wrap">
            {result.output}
          </pre>
        </ResultBlock>
      )}

      <ResultBlock title="Tool calls">
        {calls.length === 0 ? (
          <Text muted small className="px-3 py-2.5">
            This run called no tools.
          </Text>
        ) : (
          <ul className="divide-border divide-y">
            {calls.map((call, index) => {
              const outcome = outcomeLabels[call.outcome] ?? {
                label: call.outcome,
                variant: "neutral" as const,
              };
              return (
                <li
                  key={`${index}:${call.path}`}
                  className="flex items-center justify-between gap-4 px-3 py-2"
                >
                  <span
                    className="min-w-0 truncate font-mono text-xs"
                    title={call.path}
                  >
                    {call.path}
                  </span>
                  <Badge variant={outcome.variant}>
                    <Badge.Text>{outcome.label}</Badge.Text>
                  </Badge>
                </li>
              );
            })}
          </ul>
        )}
      </ResultBlock>

      {submitted !== undefined && (
        <ResultBlock title="Ran this source">
          <CodeSnippet
            language="python"
            code={submitted}
            fontSize="small"
            wordWrap
            className="max-h-48 overflow-auto border-0"
          />
        </ResultBlock>
      )}
    </section>
  );
}

function formatDuration(ms: number): string {
  return ms < 1000 ? `${Math.round(ms)} ms` : `${(ms / 1000).toFixed(1)} s`;
}

function ResultBlock({
  title,
  meta,
  children,
}: {
  title: string;
  meta?: string;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <div className="border-border border">
      <div className="border-border flex min-h-9 items-center justify-between gap-2 border-b px-3 py-2">
        <span className="text-eyebrow">{title}</span>
        {meta && (
          <Text muted small>
            {meta}
          </Text>
        )}
      </div>
      {children}
    </div>
  );
}
