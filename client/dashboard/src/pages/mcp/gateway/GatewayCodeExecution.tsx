import { useEffect, useId, useRef, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { z } from "zod";
import { Button } from "@/components/ui/Button";
import { CodeSnippet } from "@/components/ui/CodeSnippet";
import { Field, FieldLabel } from "@/components/ui/Field";
import { Text } from "@/components/ui/Text";
import { TextArea } from "@/components/ui/Textarea";
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
    retry: false,
    throwOnError: false,
  });
  const tooLarge = new TextEncoder().encode(code).length > 64 * 1024;
  const result = execution.data;

  return (
    <div className="flex flex-col gap-4">
      <Text variant="subheading">Run Python</Text>
      <Text muted small>
        Each run starts fresh and uses this connection’s permissions. Calls to
        tools can change data. The last expression is the return value.
      </Text>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          if (!disabled && !execution.isPending && code.trim() && !tooLarge)
            execution.mutate(code);
        }}
        className="flex flex-col gap-3"
      >
        <Field>
          <FieldLabel htmlFor={inputId}>Python source</FieldLabel>
          <TextArea
            id={inputId}
            value={code}
            onChange={setCode}
            rows={8}
            className="font-mono text-sm"
            disabled={execution.isPending}
          />
        </Field>
        {tooLarge && <Text small>Python source must fit within 64 KiB.</Text>}
        <div className="flex gap-2">
          <Button
            type="submit"
            disabled={
              disabled || execution.isPending || !code.trim() || tooLarge
            }
          >
            <Button.Text>
              {execution.isPending ? "Running…" : "Execute Python"}
            </Button.Text>
          </Button>
          {execution.isPending && (
            <Button
              type="button"
              variant="secondary"
              onClick={() => request.current?.()}
            >
              <Button.Text>Cancel execution</Button.Text>
            </Button>
          )}
        </div>
      </form>
      {execution.variables !== undefined && (
        <div>
          <Text variant="subheading">Submitted Python</Text>
          <CodeSnippet
            language="python"
            code={execution.variables}
            fontSize="small"
          />
        </div>
      )}
      {execution.isError && <Text role="alert">{execution.error.message}</Text>}
      {result && (
        <>
          {result.error && (
            <Text role="alert">
              {result.error.message} ({result.error.code})
            </Text>
          )}
          <div>
            <Text variant="subheading">Return value</Text>
            <CodeSnippet
              language="json"
              code={JSON.stringify(result.value ?? null, null, 2)}
              fontSize="small"
            />
          </div>
          {result.output && (
            <div>
              <Text variant="subheading">Printed output</Text>
              <pre className="font-mono text-xs break-words whitespace-pre-wrap">
                {result.output}
              </pre>
            </div>
          )}
          {result.output_truncated && (
            <Text muted small>
              Printed output was truncated.
            </Text>
          )}
          <div>
            <Text variant="subheading">Nested tool calls</Text>
            {result.tool_calls.some((call) => call.outcome === "unknown") && (
              <Text warning small>
                Some tool outcomes are unknown. Check their state before running
                again.
              </Text>
            )}
            {result.tool_calls.some(
              (call) => call.outcome === "not_dispatched",
            ) && (
              <Text muted small>
                Some tool calls were refused before dispatch.
              </Text>
            )}
            {result.tool_calls.length === 0 && (
              <Text muted small>
                No tools were called.
              </Text>
            )}
            {result.tool_calls.length > 0 && (
              <ul className="space-y-1 font-mono text-xs">
                {result.tool_calls.map((call, index) => (
                  <li key={`${index}:${call.path}`}>
                    {call.path}: {call.outcome}
                  </li>
                ))}
              </ul>
            )}
          </div>
        </>
      )}
    </div>
  );
}
