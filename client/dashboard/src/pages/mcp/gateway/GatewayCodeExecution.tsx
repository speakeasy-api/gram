import { useEffect, useId, useRef, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { z } from "zod";
import { Button } from "@/components/ui/Button";
import { CodeSnippet } from "@/components/ui/CodeSnippet";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Text } from "@/components/ui/Text";
import { TextArea } from "@/components/ui/Textarea";
import { cn } from "@/lib/utils";
import { gatewayRPC } from "./useGatewayInspection";

const resultSchema = z.object({
  value: z.unknown(),
  output: z.string(),
  output_truncated: z.boolean(),
  error: z.object({ code: z.string(), message: z.string() }).optional(),
  tool_calls: z.array(z.object({ path: z.string(), outcome: z.string() })),
});

class ExecutionNotAdmittedError extends Error {}

/** Executes only on explicit submission; transport failures never replay code. */
export function GatewayCodeExecution({
  connectUrl,
  headers,
  disabled = false,
}: {
  disabled?: boolean;
  connectUrl: string;
  headers: Record<string, string> | undefined;
}): JSX.Element {
  const inputId = useId();
  const [code, setCode] = useState('await tools.search("")');
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
            structuredContent: resultSchema.optional(),
            content: z
              .array(z.object({ text: z.string().optional() }).passthrough())
              .optional(),
            isError: z.boolean().optional(),
          })
          .parse(raw);
        if (!response.structuredContent) {
          const message =
            response.content?.[0]?.text ??
            "The gateway returned no execution result.";
          if (response.isError) throw new ExecutionNotAdmittedError(message);
          throw new Error(message);
        }
        return response.structuredContent;
      } catch (error) {
        if (error instanceof ExecutionNotAdmittedError) throw error;
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
  const result = execution.data;
  const submitted = execution.variables;
  const canRun =
    !disabled && !execution.isPending && !!code.trim() && !tooLarge;
  const run = () => {
    if (canRun) {
      startedAt.current = performance.now();
      execution.mutate(code);
    }
  };

  return (
    <section aria-label="Run Python" className="flex flex-col gap-4">
      <form
        onSubmit={(event) => {
          event.preventDefault();
          run();
        }}
        className="border-border bg-card flex flex-col border"
      >
        <div className="border-border flex min-h-[42px] items-center justify-between gap-4 border-b px-4 py-2">
          <label htmlFor={inputId} className="text-eyebrow">
            Python source
          </label>
          <Text muted small className="hidden sm:block">
            Each run starts fresh and acts with this connection&apos;s access.
          </Text>
        </div>
        <TextArea
          id={inputId}
          value={code}
          onChange={setCode}
          onKeyDown={(event) => {
            if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
              event.preventDefault();
              run();
            }
          }}
          rows={8}
          className="min-h-48 resize-y border-0 px-4 py-3 font-mono text-sm leading-relaxed focus-visible:ring-0"
          disabled={execution.isPending}
        />
        <div className="border-border flex flex-wrap items-center gap-3 border-t px-4 py-3">
          <Button type="submit" disabled={!canRun}>
            <Button.Text>
              {execution.isPending ? "Running…" : "Run Python"}
            </Button.Text>
          </Button>
          {execution.isPending && (
            <Button
              type="button"
              variant="secondary"
              onClick={() => request.current?.()}
            >
              <Button.Text>Cancel</Button.Text>
            </Button>
          )}
          <Text muted small className="ml-auto">
            {tooLarge
              ? `Source is ${formatBytes(sourceBytes)}; the limit is 64 KB.`
              : "Ctrl/⌘ Enter to run · tool calls can change data"}
          </Text>
        </div>
      </form>

      {execution.isError && (
        <Alert variant="error" dismissible={false}>
          <AlertTitle>
            {execution.error instanceof ExecutionNotAdmittedError
              ? "The run was refused"
              : "The run did not finish"}
          </AlertTitle>
          <AlertDescription>{execution.error.message}</AlertDescription>
        </Alert>
      )}

      {result && submitted !== undefined && (
        <ExecutionResult
          result={result}
          durationMs={durationMs}
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
  submitted,
}: {
  result: z.infer<typeof resultSchema>;
  durationMs: number | undefined;
  /** The source that produced this result, when the editor has moved on. */
  submitted: string | undefined;
}): JSX.Element {
  const calls = result.tool_calls;
  const summary = [
    result.error ? "Failed" : "Returned",
    `${calls.length} tool ${calls.length === 1 ? "call" : "calls"}`,
    durationMs === undefined ? undefined : formatDuration(durationMs),
  ]
    .filter(Boolean)
    .join(" · ");
  const unknownOutcomes = calls.some((call) => call.outcome === "unknown");

  return (
    <div className="flex flex-col gap-4" aria-live="polite">
      <div className="flex items-center gap-2">
        <span
          aria-hidden
          className={cn(
            "size-2 rounded-full",
            result.error ? "bg-destructive" : "bg-success",
          )}
        />
        <Text small>{summary}</Text>
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
          className="max-h-[28rem] overflow-auto"
        />
      </ResultBlock>

      {(result.output || result.output_truncated) && (
        <ResultBlock
          title="Printed output"
          meta={result.output_truncated ? "Truncated" : undefined}
        >
          <pre className="bg-muted/20 max-h-64 overflow-auto px-4 py-3 font-mono text-xs leading-relaxed break-words whitespace-pre-wrap">
            {result.output}
          </pre>
        </ResultBlock>
      )}

      <ResultBlock title="Tool calls">
        {calls.length === 0 ? (
          <Text muted small className="px-4 py-3">
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
                  className="flex items-center justify-between gap-4 px-4 py-2"
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
            className="max-h-48 overflow-auto"
          />
        </ResultBlock>
      )}
    </div>
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
    <div className="border-border bg-card border">
      <div className="border-border flex min-h-[38px] items-center justify-between gap-2 border-b px-4 py-2">
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
