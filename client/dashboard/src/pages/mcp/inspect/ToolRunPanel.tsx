import { Button } from "@/components/ui/Button";
import { ToolResponseView } from "./ToolResponseView";
import { toast } from "sonner";
import { CodeSnippet } from "@/components/ui/CodeSnippet";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { Input } from "@/components/ui/Input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Skeleton } from "@/components/ui/Skeleton";
import { StatusDot } from "@/components/ui/StatusDot";
import { Switch } from "@/components/ui/Switch";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/Tooltip";
import { cn } from "@/lib/utils";
import { useMutation } from "@tanstack/react-query";
import { Check, ChevronRight, Copy, Info } from "lucide-react";
import {
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { Link } from "react-router";
import type { McpToolRun } from "./runMcpTool";
import {
  extractFields,
  isJsonField,
  jsonFieldError,
  optionKey,
  toArguments,
  type ToolField,
} from "./toolSchemaFields";
import {
  isDestructive,
  unavailableReasonLabel,
  type InspectTool,
  type ToolDraft,
} from "./types";
import {
  HEALTH_WINDOW_LABEL,
  isFailing,
  type ToolHealth,
} from "./useToolHealth";

function withoutKey(
  record: Record<string, string>,
  key: string,
): Record<string, string> {
  const next = { ...record };
  delete next[key];
  return next;
}

/**
 * The form for one tool, built from its input schema, plus the outcome of the
 * last run. Runs only on an explicit click; destructive tools confirm first.
 * The draft lives with the caller, so switching tabs or tools keeps it.
 */
export function ToolRunPanel({
  tool,
  health,
  isHealthLoading,
  run,
  accessHref,
  draft,
  onDraftChange,
}: {
  tool: InspectTool;
  health: ToolHealth | undefined;
  isHealthLoading?: boolean;
  /** Where the rule that blocks this tool can be changed. */
  accessHref?: string;
  /** Undefined when this panel can only describe the tool, not run it. */
  run: ((args: Record<string, unknown>) => Promise<McpToolRun>) | undefined;
  draft: ToolDraft;
  onDraftChange: (update: (draft: ToolDraft) => ToolDraft) => void;
}): JSX.Element {
  const fields = useMemo(() => extractFields(tool.inputSchema), [tool]);
  // Required arguments first; the rest keep the schema's order.
  const visibleFields = useMemo(
    () =>
      fields
        .filter((field) => field.kind !== "const")
        .sort((a, b) => Number(b.required) - Number(a.required)),
    [fields],
  );
  const values = draft.values;
  const [confirming, setConfirming] = useState(false);
  const [formError, setFormError] = useState<string>();
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const responseRef = useRef<HTMLDivElement>(null);
  const justRan = useRef(false);
  const execution = useMutation({
    mutationFn: (args: Record<string, unknown>) => {
      if (!run) throw new Error("This tool can't be run here.");
      return run(args);
    },
    onSuccess: (result) => {
      justRan.current = true;
      onDraftChange((current) => ({ ...current, result }));
    },
  });

  // A finished run lands below the form: bring its header into view and move
  // focus there, so keyboard and screen reader users hear the outcome.
  useEffect(() => {
    if (!justRan.current || !draft.result) return;
    justRan.current = false;
    const header = responseRef.current;
    if (!header) return;
    header.scrollIntoView?.({ block: "nearest" });
    header.focus({ preventScroll: true });
  }, [draft.result]);

  const destructive = isDestructive(tool);
  const blocked = !!tool.unavailable;
  const canRun = !!run && !blocked && !execution.isPending;

  const submit = () => {
    const errors: Record<string, string> = {};
    for (const field of visibleFields) {
      const error = jsonFieldError(field, values[field.name] ?? "");
      if (error) errors[field.name] = error;
    }
    setFieldErrors(errors);
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

  const footerError =
    formError ?? (execution.error ? execution.error.message : undefined);

  return (
    <div className="flex min-h-full flex-col">
      <div className="flex flex-1 flex-col gap-5 px-6 py-5">
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
          <HealthLine health={health} isLoading={isHealthLoading} />
          {blocked ? (
            <p className="text-sm">
              <span className="text-destructive">
                {unavailableReasonLabel(tool.unavailable?.reason)}, so you
                can&apos;t run this tool.
              </span>{" "}
              {accessHref ? (
                <Link
                  to={accessHref}
                  className="text-foreground font-medium underline underline-offset-2"
                >
                  Review access
                </Link>
              ) : (
                <span className="text-muted-foreground">
                  Ask an admin of this server for access.
                </span>
              )}
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
                error={fieldErrors[field.name]}
                disabled={!canRun}
                onChange={(value) => {
                  onDraftChange((current) => ({
                    ...current,
                    values: { ...current.values, [field.name]: value },
                  }));
                  if (fieldErrors[field.name])
                    setFieldErrors((current) =>
                      withoutKey(current, field.name),
                    );
                }}
                onBlur={(value) => {
                  const error = jsonFieldError(field, value);
                  setFieldErrors((current) =>
                    error
                      ? { ...current, [field.name]: error }
                      : withoutKey(current, field.name),
                  );
                }}
              />
            ))
          )}
          <div className="flex items-center justify-end gap-3">
            {footerError ? (
              <p role="alert" className="text-destructive mr-auto text-xs">
                {footerError}
              </p>
            ) : null}
            <Button
              type="submit"
              size="sm"
              className="font-sans text-[13px] font-medium tracking-normal normal-case"
              disabled={!canRun}
            >
              <Button.Text>
                {execution.isPending ? "Executing…" : "Execute tool"}
              </Button.Text>
            </Button>
          </div>
        </form>

        {draft.result ? (
          <RunResult run={draft.result} headerRef={responseRef} />
        ) : null}
      </div>

      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title={`Execute ${tool.name}?`}
        description="This tool is marked destructive. It runs for real, as you."
        confirmLabel="Execute tool"
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

/** A failure rate: the number in mono, the words in the sentence's font. */
export function FailedRate({
  rate,
  className,
}: {
  rate: number;
  className?: string;
}): JSX.Element {
  return (
    <span className={cn("font-sans", className)}>
      <span className="font-mono tabular-nums">{(rate * 100).toFixed(1)}%</span>{" "}
      failed
    </span>
  );
}

function HealthLine({
  health,
  isLoading,
}: {
  health: ToolHealth | undefined;
  isLoading?: boolean;
}): JSX.Element {
  if (isLoading) {
    return <Skeleton className="h-4 w-48" />;
  }
  if (!health || health.calls === 0) {
    return (
      <p className="text-muted-foreground font-sans text-xs">
        No calls in the {HEALTH_WINDOW_LABEL}
      </p>
    );
  }
  return (
    <p className="text-muted-foreground font-sans text-xs">
      <span className="font-mono tabular-nums">
        {health.calls.toLocaleString()}
      </span>{" "}
      {health.calls === 1 ? "call" : "calls"}
      {health.failureRate > 0 ? (
        <>
          {" "}
          ·{" "}
          <FailedRate
            rate={health.failureRate}
            className={cn(isFailing(health) && "text-destructive")}
          />
        </>
      ) : null}{" "}
      in the {HEALTH_WINDOW_LABEL}
    </p>
  );
}

export function FieldInput({
  field,
  value,
  error,
  disabled,
  onChange,
  onBlur,
}: {
  field: ToolField;
  value: string;
  error: string | undefined;
  disabled: boolean;
  onChange: (value: string) => void;
  onBlur: (value: string) => void;
}): JSX.Element {
  const id = useId();
  const labelId = `${id}-label`;
  const errorId = `${id}-error`;
  const label = (
    <span className="flex items-baseline justify-between gap-2">
      <label id={labelId} htmlFor={id} className="font-mono text-xs">
        {field.name}
        {field.required ? <span className="text-destructive"> *</span> : null}
      </label>
      <span className="text-muted-foreground flex items-center gap-1 font-mono text-[11px]">
        {field.typeLabel}
        {field.schema !== undefined &&
        (isJsonField(field) || field.kind === "text-or-json") ? (
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
  const errorProps = error
    ? { "aria-invalid": true, "aria-describedby": errorId }
    : {};

  // One field look for every kind: the Input frame, 36px tall for one line.
  let control: ReactNode;
  switch (field.kind) {
    case "const":
      // Never rendered: the panel filters these out and sends the value.
      control = null;
      break;
    case "enum":
      control = (
        <Select value={value} onValueChange={onChange} disabled={disabled}>
          <SelectTrigger
            id={id}
            className="bg-surface-primary-default h-9 w-full px-3"
          >
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
        <Input
          id={id}
          multiline
          lines={field.kind === "arguments" ? 6 : 3}
          value={value}
          onChange={onChange}
          onBlur={(event) => onBlur(event.target.value)}
          disabled={disabled}
          error={!!error}
          placeholder={field.example ?? "{}"}
          className="px-3 py-1.5 font-mono"
          {...errorProps}
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
          onBlur={(event) => onBlur(event.target.value)}
          disabled={disabled}
          error={!!error}
          type={field.kind === "number" ? "number" : "text"}
          placeholder={
            field.kind === "text-or-json" && field.example
              ? `Text, or JSON like ${field.example}`
              : undefined
          }
          className="h-9 px-3 py-0"
          {...errorProps}
        />
      );
  }

  return (
    <div className="flex flex-col gap-1.5">
      {label}
      {control}
      {error ? (
        <p id={errorId} className="text-destructive text-xs leading-4">
          {error}
        </p>
      ) : null}
      {hint}
    </div>
  );
}

function RunResult({
  run,
  headerRef,
}: {
  run: McpToolRun;
  headerRef: React.RefObject<HTMLDivElement | null>;
}): JSX.Element {
  const [open, setOpen] = useState(false);
  const value =
    run.value === undefined ? undefined : JSON.stringify(run.value, null, 2);

  return (
    <section
      aria-label="Response"
      className="border-border flex flex-col gap-3 border-t pt-5"
    >
      <div
        ref={headerRef}
        tabIndex={-1}
        className="focus-visible:outline-foreground flex items-center justify-between gap-2 outline-none focus-visible:outline-1 focus-visible:outline-offset-2"
      >
        <span className="text-eyebrow">Response</span>
        <span className="flex items-center gap-2 text-xs">
          <StatusDot tone={run.ok ? "success" : "destructive"} />
          <span className={cn(!run.ok && "text-destructive")}>
            {run.ok ? "Succeeded" : "Failed"}
          </span>
          <span className="text-muted-foreground">
            <span className="font-mono tabular-nums">
              {run.durationMs.toLocaleString()}
            </span>{" "}
            ms
          </span>
        </span>
      </div>
      <div className="border-border flex flex-col border">
        {run.ok ? (
          <ToolResponseView
            // Text that is itself JSON reads better as JSON.
            text={isJson(run.summary) ? undefined : run.summary}
            json={value}
          />
        ) : null}
        {!run.ok && run.summary ? (
          <p
            className={cn(
              "px-3 py-2.5 text-[13px] break-words whitespace-pre-wrap",
              !run.ok && "text-destructive",
            )}
          >
            {run.summary}
          </p>
        ) : null}
        {!run.ok && value !== undefined ? (
          <div className="max-h-80 overflow-auto">
            <CodeSnippet
              language="json"
              code={value}
              fontSize="small"
              wordWrap
              className="border-0"
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
                <CopyTrace traceId={run.traceId} />
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
              className="border-0"
            />
          </div>
        ) : null}
      </div>
    </section>
  );
}

function isJson(text: string | undefined): boolean {
  if (!text) return false;
  try {
    JSON.parse(text);
    return true;
  } catch {
    return false;
  }
}

const COPIED_MS = 1500;

/**
 * Copies the full trace ID. The tooltip opens above, clear of the "Trace"
 * label, and says "Copied" for a moment after a click.
 */
function CopyTrace({ traceId }: { traceId: string }): JSX.Element {
  const [copied, setCopied] = useState(false);
  const timer = useRef<number | undefined>(undefined);
  useEffect(() => () => window.clearTimeout(timer.current), []);
  return (
    <>
      <Tooltip open={copied ? true : undefined}>
        <TooltipTrigger asChild>
          <button
            type="button"
            aria-label="Copy trace ID"
            onClick={() => {
              // "Copied" only once the clipboard took it.
              void navigator.clipboard
                ?.writeText(traceId)
                .then(() => {
                  setCopied(true);
                  window.clearTimeout(timer.current);
                  timer.current = window.setTimeout(
                    () => setCopied(false),
                    COPIED_MS,
                  );
                })
                .catch(() => toast.error("Couldn't copy the trace ID."));
            }}
            className="hover:text-foreground flex size-6 items-center justify-center"
          >
            {copied ? (
              <Check className="size-3.5" />
            ) : (
              <Copy className="size-3.5" />
            )}
          </button>
        </TooltipTrigger>
        <TooltipContent side="top" align="end">
          {copied ? "Copied" : "Copy trace ID"}
        </TooltipContent>
      </Tooltip>
      <span className="sr-only" aria-live="polite">
        {copied ? "Copied" : ""}
      </span>
    </>
  );
}
