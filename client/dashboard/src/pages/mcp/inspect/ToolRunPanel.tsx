import { Button } from "@/components/ui/Button";
import { CodeSnippet } from "@/components/ui/CodeSnippet";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { CopyButton } from "@/components/ui/CopyButton";
import { Input } from "@/components/ui/Input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { StatusDot } from "@/components/ui/StatusDot";
import { Switch } from "@/components/ui/Switch";
import { TextArea } from "@/components/ui/Textarea";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/Tooltip";
import { cn } from "@/lib/utils";
import { useMutation } from "@tanstack/react-query";
import { ChevronRight, Info } from "lucide-react";
import { useId, useMemo, useState, type ReactNode } from "react";
import type { McpToolRun } from "./runMcpTool";
import {
  extractFields,
  optionKey,
  toArguments,
  type ToolField,
} from "./toolSchemaFields";
import { isDestructive, type InspectTool } from "./types";
import {
  HEALTH_WINDOW_LABEL,
  isFailing,
  type ToolHealth,
} from "./useToolHealth";

/**
 * The form for one tool, built from its input schema, plus the outcome of the
 * last run. Runs only on an explicit click; destructive tools confirm first.
 */
export function ToolRunPanel({
  tool,
  toolbar,
  health,
  run,
}: {
  tool: InspectTool;
  /** Sits left of "Run as you" on the panel's top row. */
  toolbar?: ReactNode;
  health: ToolHealth | undefined;
  /** Undefined when this panel can only describe the tool, not run it. */
  run: ((args: Record<string, unknown>) => Promise<McpToolRun>) | undefined;
}): JSX.Element {
  const fields = useMemo(() => extractFields(tool.inputSchema), [tool]);
  const visibleFields = fields.filter((field) => field.kind !== "const");
  const [values, setValues] = useState<Record<string, string>>({});
  const [confirming, setConfirming] = useState(false);
  const [formError, setFormError] = useState<string>();
  const execution = useMutation({
    mutationFn: (args: Record<string, unknown>) => {
      if (!run) throw new Error("This tool can't be run here.");
      return run(args);
    },
  });

  const destructive = isDestructive(tool);
  const blocked = !!tool.unavailable;
  const canRun = !!run && !blocked && !execution.isPending;

  const submit = () => {
    const parsed = toArguments(fields, values);
    if (!parsed.ok) {
      setFormError(parsed.error);
      return;
    }
    setFormError(undefined);
    if (destructive) {
      setConfirming(true);
      return;
    }
    execution.mutate(parsed.args);
  };

  return (
    <div className="flex flex-col gap-5">
      <div className="flex items-center justify-between gap-3">
        {toolbar}
        <Button
          size="sm"
          className="font-sans text-[13px] font-medium tracking-normal normal-case"
          disabled={!canRun}
          onClick={submit}
          aria-label={`Run ${tool.name} as you`}
        >
          <Button.Text>
            {execution.isPending ? "Running…" : "Run as you"}
          </Button.Text>
        </Button>
      </div>
      <div className="flex flex-col gap-1.5">
        <span className="flex min-w-0 flex-wrap items-center gap-2">
          <span className="min-w-0 font-mono text-sm font-medium break-all">
            {tool.name}
          </span>
          {destructive ? (
            <span className="text-muted-foreground font-mono text-[10px] leading-4 tracking-wide uppercase">
              Destructive
            </span>
          ) : null}
        </span>
        {tool.description ? (
          <p className="text-muted-foreground line-clamp-4 text-[13px] leading-5 whitespace-pre-line">
            {tool.description}
          </p>
        ) : null}
        <HealthLine health={health} />
        {blocked ? (
          <p className="text-destructive text-sm">
            You can&apos;t run this tool. Its access rule is shown in the list.
          </p>
        ) : null}
      </div>

      <form
        aria-label="Arguments"
        className="border-border flex flex-col gap-4 border-t pt-5"
        onSubmit={(event) => {
          event.preventDefault();
          submit();
        }}
      >
        <span className="text-eyebrow">Arguments</span>
        {visibleFields.length === 0 ? (
          <p className="text-muted-foreground text-[13px]">
            This tool takes no arguments.
          </p>
        ) : (
          visibleFields.map((field) => (
            <FieldInput
              key={field.name}
              field={field}
              value={values[field.name] ?? ""}
              disabled={!canRun}
              onChange={(value) =>
                setValues((current) => ({ ...current, [field.name]: value }))
              }
            />
          ))
        )}
        {formError ? (
          <p className="text-destructive text-sm">{formError}</p>
        ) : null}
      </form>

      {execution.data ? <RunResult run={execution.data} /> : null}

      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title={`Run ${tool.name}?`}
        description="This tool is marked destructive. It runs for real, with your own account."
        confirmLabel="Run tool"
        confirmVariant="destructive-primary"
        onConfirm={() => {
          setConfirming(false);
          const parsed = toArguments(fields, values);
          if (parsed.ok) execution.mutate(parsed.args);
        }}
      />
    </div>
  );
}

function HealthLine({
  health,
}: {
  health: ToolHealth | undefined;
}): JSX.Element {
  if (!health || health.calls === 0) {
    return (
      <p className="text-muted-foreground text-xs">
        No calls in the {HEALTH_WINDOW_LABEL}
      </p>
    );
  }
  const rate = `${(health.failureRate * 100).toFixed(1)}%`;
  return (
    <p className="text-muted-foreground text-xs tabular-nums">
      {health.calls.toLocaleString()} calls
      {health.failureRate > 0 ? (
        <>
          {" "}
          ·{" "}
          <span className={cn(isFailing(health) && "text-destructive")}>
            {rate} failed
          </span>
        </>
      ) : null}{" "}
      in the {HEALTH_WINDOW_LABEL}
    </p>
  );
}

function FieldInput({
  field,
  value,
  disabled,
  onChange,
}: {
  field: ToolField;
  value: string;
  disabled: boolean;
  onChange: (value: string) => void;
}): JSX.Element {
  const id = useId();
  const labelId = `${id}-label`;
  const label = (
    <span className="flex items-baseline justify-between gap-2">
      <label id={labelId} htmlFor={id} className="font-mono text-xs">
        {field.name}
        {field.required ? <span className="text-destructive"> *</span> : null}
      </label>
      <span className="text-muted-foreground flex items-center gap-1 font-mono text-[11px]">
        {field.typeLabel}
        {field.schema !== undefined &&
        (field.kind === "json" ||
          field.kind === "text-or-json" ||
          field.kind === "arguments") ? (
          <Tooltip>
            <TooltipTrigger
              type="button"
              aria-label={`Schema for ${field.name}`}
              className="hover:text-foreground"
            >
              <Info className="size-3" />
            </TooltipTrigger>
            <TooltipContent className="max-w-sm">
              <pre className="font-mono text-[11px] leading-4 break-words whitespace-pre-wrap">
                {JSON.stringify(field.schema, null, 2)}
              </pre>
            </TooltipContent>
          </Tooltip>
        ) : null}
      </span>
    </span>
  );
  const hint = field.description ? (
    <p className="text-muted-foreground text-xs leading-4">
      {field.description}
    </p>
  ) : null;

  let control: JSX.Element | null;
  switch (field.kind) {
    case "const":
      // Never rendered: the panel filters these out and sends the value.
      control = null;
      break;
    case "enum":
      control = (
        <Select value={value} onValueChange={onChange} disabled={disabled}>
          <SelectTrigger id={id} className="w-full">
            <SelectValue placeholder="Choose a value" />
          </SelectTrigger>
          <SelectContent>
            {field.options.map((option) => (
              <SelectItem
                key={optionKey(option.value)}
                value={optionKey(option.value)}
              >
                {option.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      );
      break;
    case "boolean":
      control = (
        <Switch
          checked={value === "true"}
          onCheckedChange={(checked) => onChange(String(checked))}
          disabled={disabled}
          aria-labelledby={labelId}
        />
      );
      break;
    case "json":
    case "arguments":
      control = (
        <TextArea
          id={id}
          value={value}
          onChange={onChange}
          disabled={disabled}
          rows={field.kind === "arguments" ? 6 : 3}
          placeholder={field.example ?? "{}"}
          className="font-mono text-xs"
        />
      );
      break;
    case "string":
    case "number":
    case "text-or-json":
      control = (
        <Input
          id={id}
          value={value}
          onChange={onChange}
          disabled={disabled}
          type={field.kind === "number" ? "number" : "text"}
          placeholder={
            field.kind === "text-or-json" && field.example
              ? `Text, or JSON like ${field.example}`
              : undefined
          }
        />
      );
  }

  return (
    <div className="flex flex-col gap-1.5">
      {label}
      {control}
      {hint}
    </div>
  );
}

function RunResult({ run }: { run: McpToolRun }): JSX.Element {
  const [open, setOpen] = useState(false);
  const value =
    run.value === undefined ? undefined : JSON.stringify(run.value, null, 2);

  return (
    <section
      aria-label="Response"
      className="border-border flex flex-col gap-3 border-t pt-5"
    >
      <div className="flex items-center justify-between gap-2">
        <span className="text-eyebrow">Response</span>
        <span className="flex items-center gap-2 text-xs">
          <StatusDot tone={run.ok ? "success" : "destructive"} />
          <span className={cn(!run.ok && "text-destructive")}>
            {run.ok ? "Succeeded" : "Failed"}
          </span>
          <span className="text-muted-foreground tabular-nums">
            {run.durationMs.toLocaleString()} ms
          </span>
        </span>
      </div>
      <div className="border-border flex flex-col border">
        {run.summary && (!run.ok || value === undefined) ? (
          <p
            className={cn(
              "px-3 py-2.5 text-[13px] break-words whitespace-pre-wrap",
              !run.ok && "text-destructive",
            )}
          >
            {run.summary}
          </p>
        ) : null}
        {value !== undefined ? (
          <div className="max-h-80 overflow-auto">
            <CodeSnippet
              language="json"
              code={value}
              fontSize="small"
              wordWrap
            />
          </div>
        ) : null}
        {run.exchange || run.traceId ? (
          <div className="border-border bg-muted/30 flex flex-wrap items-center justify-between gap-2 border-t px-3 py-2 text-xs">
            {run.exchange ? (
              <button
                type="button"
                aria-expanded={open}
                onClick={() => setOpen((value) => !value)}
                className="text-muted-foreground hover:text-foreground flex items-center gap-1"
              >
                <ChevronRight
                  className={cn(
                    "size-3.5 transition-transform",
                    open && "rotate-90",
                  )}
                />
                Request and response
              </button>
            ) : (
              <span />
            )}
            {run.traceId ? (
              <span className="text-muted-foreground flex items-center gap-1">
                Trace{" "}
                <span className="font-mono">{run.traceId.slice(0, 12)}…</span>
                <CopyButton
                  text={run.traceId}
                  size="xs"
                  tooltip="Copy trace ID"
                />
              </span>
            ) : null}
          </div>
        ) : null}
        {open && run.exchange ? (
          <div className="border-border max-h-96 overflow-auto border-t">
            <CodeSnippet
              language="json"
              code={JSON.stringify(
                {
                  request: run.exchange.request,
                  status: run.exchange.status,
                  response: run.exchange.response,
                },
                null,
                2,
              )}
              fontSize="small"
              wordWrap
            />
          </div>
        ) : null}
      </div>
    </section>
  );
}
