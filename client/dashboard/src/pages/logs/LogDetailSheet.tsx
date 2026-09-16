import { MCPCard, MCPCardSkeleton } from "@/components/mcp/MCPCard";
import { Card } from "@/components/ui/Card";
import { useToolsets } from "@/pages/toolsets/useToolsets";
import { useRoutes } from "@/routes";
import { Sheet, SheetContent, SheetTitle } from "@/components/ui/Sheet";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/Tabs";
import { cn } from "@/lib/utils";
import { TelemetryLogRecord } from "@gram/client/models/components/telemetrylogrecord.js";
import { Operator } from "@gram/client/models/components/logfilter";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/Dropdown";
import { Check, ChevronDown, ChevronRight, Copy } from "lucide-react";
import { useEffect, useId, useMemo, useState } from "react";
import {
  DARK_THEME,
  highlightCode,
  type CodeLine,
} from "@/components/ui/lib/codeUtils";
import { ErrorBoundary } from "react-error-boundary";
import { formatNanoTimestamp } from "./utils";

interface LogDetailSheetProps {
  log: TelemetryLogRecord | null;
  hostedToolsetSlug?: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onAddFilter?: (path: string, op: Operator, value: string) => void;
}

export function LogDetailSheet({
  log,
  hostedToolsetSlug,
  open,
  onOpenChange,
  onAddFilter,
}: LogDetailSheetProps): JSX.Element {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        className="h-full max-h-screen overflow-y-auto"
        style={{ width: "33vw", minWidth: 500, maxWidth: "none" }}
      >
        {log && (
          <LogDetailContent
            log={log}
            hostedToolsetSlug={hostedToolsetSlug}
            onAddFilter={onAddFilter}
          />
        )}
      </SheetContent>
    </Sheet>
  );
}

/** Keys used to store tool I/O content in telemetry attributes (OTel GenAI semantic conventions). */
const TOOL_IO_ATTR_KEYS = {
  input: "gen_ai.tool.call.arguments",
  output: "gen_ai.tool.call.result",
} as const;

const HOOK_BLOCK_REASON_KEY = "gram.hook.block_reason";

const HOOK_ERROR_KEY = "gram.hook.error";

/**
 * Extract a deeply nested value from an object using a dot-separated path.
 * e.g. getNestedValue(obj, "gram.tool_call.input.content")
 */
function getNestedValue(
  obj: Record<string, unknown>,
  path: string,
): string | undefined {
  const parts = path.split(".");
  let current: unknown = obj;
  for (const part of parts) {
    if (
      current === null ||
      current === undefined ||
      typeof current !== "object"
    )
      return undefined;
    current = (current as Record<string, unknown>)[part];
  }
  return typeof current === "string" ? current : undefined;
}

/**
 * Remove a deeply nested key from an object (mutates a cloned copy).
 * Returns a shallow-cloned object with the leaf key removed.
 */
function removeNestedKey(
  obj: Record<string, unknown>,
  path: string,
): Record<string, unknown> {
  const clone = structuredClone(obj);
  const parts = path.split(".");
  let current: Record<string, unknown> = clone;
  for (let i = 0; i < parts.length - 1; i++) {
    const next = current[parts[i]!];
    if (next === null || next === undefined || typeof next !== "object")
      return clone;
    current = next as Record<string, unknown>;
  }
  delete current[parts[parts.length - 1]!];
  return clone;
}

function HostedServerCard({
  toolsetSlug,
}: {
  toolsetSlug: string;
}): JSX.Element {
  const toolsets = useToolsets();
  const routes = useRoutes();
  const toolset = toolsets.find((item) => item.slug === toolsetSlug);
  const unavailableCard = (
    <Card href={routes.mcp.details.overview.href(toolsetSlug)}>
      <Card.Header>
        <Card.Title>{toolsetSlug}</Card.Title>
        <Card.Description>Server details are unavailable</Card.Description>
      </Card.Header>
    </Card>
  );
  let card = unavailableCard;
  if (toolset) {
    card = (
      <ErrorBoundary fallback={unavailableCard} resetKeys={[toolsetSlug]}>
        <MCPCard toolset={toolset} />
      </ErrorBoundary>
    );
  } else if (toolsets.isLoading) {
    card = <MCPCardSkeleton />;
  }
  return (
    <section aria-label="Hosted MCP server" className="flex flex-col gap-2">
      <h3 className="text-eyebrow">Hosted MCP server</h3>
      {card}
    </section>
  );
}

function LogDetailContent({
  log,
  hostedToolsetSlug,
  onAddFilter,
}: {
  log: TelemetryLogRecord;
  hostedToolsetSlug?: string;
  onAddFilter?: (path: string, op: Operator, value: string) => void;
}) {
  const resourceAttrs = log.resourceAttributes as
    | { gram?: { tool?: { urn?: string } } }
    | undefined;
  const gramUrn = resourceAttrs?.gram?.tool?.urn;

  // Extract tool I/O content from attributes
  const attrs = log.attributes as Record<string, unknown> | undefined;
  const flatToolsetSlug = attrs?.["gram.toolset.slug"];
  const toolsetSlug =
    (attrs && getNestedValue(attrs, "gram.toolset.slug")) ||
    (typeof flatToolsetSlug === "string" && flatToolsetSlug) ||
    hostedToolsetSlug;
  const toolInput = attrs
    ? getNestedValue(attrs, TOOL_IO_ATTR_KEYS.input)
    : undefined;
  const toolOutput = attrs
    ? getNestedValue(attrs, TOOL_IO_ATTR_KEYS.output)
    : undefined;
  const blockReason = attrs
    ? getNestedValue(attrs, HOOK_BLOCK_REASON_KEY)
    : undefined;
  const toolError = attrs ? getNestedValue(attrs, HOOK_ERROR_KEY) : undefined;
  const toolCallID = attrs ? getNestedValue(attrs, "gen_ai.tool.call.id") : "";
  const toolName = attrs ? getNestedValue(attrs, "gram.tool.name") : "";
  const showToolIOHiddenMessage = Boolean(
    (toolCallID || toolName) && !toolInput,
  );
  // Remove surfaced keys from attributes to avoid duplication in the generic section
  let filteredAttrs = attrs;
  if (filteredAttrs && toolInput) {
    filteredAttrs = removeNestedKey(filteredAttrs, TOOL_IO_ATTR_KEYS.input);
  }
  if (filteredAttrs && toolOutput) {
    filteredAttrs = removeNestedKey(filteredAttrs, TOOL_IO_ATTR_KEYS.output);
  }
  if (filteredAttrs && blockReason) {
    filteredAttrs = removeNestedKey(filteredAttrs, HOOK_BLOCK_REASON_KEY);
  }
  if (filteredAttrs && toolError) {
    filteredAttrs = removeNestedKey(filteredAttrs, HOOK_ERROR_KEY);
  }

  return (
    <div className="flex flex-col gap-6 px-5 pt-6 pb-6">
      {/* Header — severity word + headline, then a hairline-ruled meta list */}
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-2">
          {/* Same primitives as the table row this was opened from: a dot for
              the outcome, the tool in mono. Landing on a different visual
              language than the row you clicked makes them feel unrelated. */}
          <div className="flex items-center gap-2">
            <span
              aria-hidden
              className={cn(
                "size-1.5 shrink-0 rounded-full",
                blockReason || toolError ? "bg-rose-500" : "bg-emerald-500",
              )}
            />
            <span
              className={cn(
                "font-mono text-xs tracking-wide uppercase",
                blockReason || toolError
                  ? "text-destructive"
                  : "text-muted-foreground",
              )}
            >
              {blockReason ? "Blocked" : toolError ? "Error" : "Success"}
            </span>
            <span className="text-muted-foreground font-mono text-xs">
              {formatNanoTimestamp(log.timeUnixNano)}
            </span>
          </div>
          <SheetTitle className="flex items-baseline gap-1.5 font-mono text-base font-medium">
            {toolsetSlug && (
              <span className="text-muted-foreground">{toolsetSlug} /</span>
            )}
            <span>{toolName || log.body?.slice(0, 60) || "(no message)"}</span>
          </SheetTitle>
        </div>

        {blockReason && (
          <div className="flex items-start gap-3 border-l-2 border-l-[var(--color-feedback-orange-600)] py-1 pl-3">
            <div className="flex min-w-0 flex-1 flex-col gap-1">
              <div className="font-mono text-xs tracking-wide uppercase text-[var(--color-feedback-orange-600)] dark:text-[var(--color-feedback-orange-400)]">
                Block Reason
              </div>
              <div className="text-foreground text-sm break-words">
                {blockReason}
              </div>
            </div>
          </div>
        )}
      </div>

      {toolsetSlug && <HostedServerCard toolsetSlug={toolsetSlug} />}

      {/* Tabs: Details / Raw Data */}
      <Tabs defaultValue="details" className="w-full flex-1">
        <TabsList className="w-full">
          <TabsTrigger value="details" className="flex-1">
            Details
          </TabsTrigger>
          <TabsTrigger value="raw" className="flex-1">
            Raw Data
          </TabsTrigger>
        </TabsList>

        <TabsContent value="details" className="mt-5 flex flex-col gap-5">
          {/* What happened, then what went in, then what came back. Everything
              else — the identity, hook and project attributes that were filling
              the sheet before you could reach the payload — sits behind one
              disclosure below. */}
          {toolError && (
            <div className="border-l-destructive flex items-start gap-3 border-l-2 py-1 pl-3">
              <div className="flex min-w-0 flex-1 flex-col gap-1">
                <div className="text-destructive-default font-mono text-xs tracking-wide uppercase">
                  Error
                </div>
                <div className="text-foreground text-sm break-words">
                  {toolError}
                </div>
              </div>
            </div>
          )}

          {toolInput && (
            <CollapsibleBodySection title="Arguments" content={toolInput} />
          )}
          {showToolIOHiddenMessage && (
            <div className="text-muted-foreground border-border border px-3 py-2 text-sm">
              Tool arguments are not shown when tool_io_logs are disabled.
            </div>
          )}

          {toolOutput && (
            <CollapsibleBodySection title="Result" content={toolOutput} />
          )}

          <CollapsibleSection title="Context" defaultOpen={false}>
            <div className="border-border divide-border flex flex-col divide-y border-y">
              <MetadataRow
                label="Service"
                value={log.service?.name || "Unknown"}
              />
              {gramUrn && (
                <MetadataRow
                  label="Platform URN"
                  value={gramUrn}
                  copyValue={gramUrn}
                />
              )}
              {log.traceId && (
                <MetadataRow
                  label="Trace ID"
                  value={log.traceId}
                  copyValue={log.traceId}
                />
              )}
              {log.spanId && (
                <MetadataRow
                  label="Span ID"
                  value={log.spanId}
                  copyValue={log.spanId}
                />
              )}
            </div>

            {filteredAttrs && Object.keys(filteredAttrs).length > 0 && (
              <AttributesSection
                title="Attributes"
                data={filteredAttrs}
                onAddFilter={onAddFilter}
              />
            )}

            {/* Resource — no onAddFilter: the backend's attribute filter
                resolves paths against `attributes.*`, not
                `resource_attributes.*`, so resource-derived filters would
                silently return no results. */}
            {log.resourceAttributes &&
              Object.keys(log.resourceAttributes as object).length > 0 && (
                <AttributesSection
                  title="Resource"
                  data={log.resourceAttributes as Record<string, unknown>}
                />
              )}

            {/* The OTEL log body, which for tool-call events is a
                "Tool: X, Hook: Y" stub duplicating what is shown above. */}
            {log.body && (
              <CollapsibleBodySection
                title="Message"
                content={log.body}
                defaultOpen={false}
              />
            )}
          </CollapsibleSection>
        </TabsContent>

        <TabsContent value="raw" className="mt-5 flex flex-col gap-3">
          <div className="flex items-center justify-between">
            <div className="text-eyebrow">Full Log Record</div>
            <CopyIconButton
              value={JSON.stringify(log, null, 2)}
              label="log record"
            />
          </div>
          <div className="border-border flex-1 overflow-y-auto border">
            <CodeBlock content={JSON.stringify(log, null, 2)} />
          </div>
        </TabsContent>
      </Tabs>
    </div>
  );
}

/**
 * A payload rendered the way a code editor renders it: Shiki tokens on a dark
 * ground. Always dark, whichever theme the dashboard is in — these blocks are
 * quoted machine output, and the shift in ground is what separates them from
 * the sheet's own prose.
 */
function CodeBlock({ content }: { content: string }) {
  const [lines, setLines] = useState<CodeLine[] | null>(null);

  // Not every payload is JSON: a hook message body is plain text, and asking
  // the JSON grammar to tokenize it produces a wall of error scopes.
  const language = useMemo(() => {
    const trimmed = content.trimStart();
    return trimmed.startsWith("{") || trimmed.startsWith("[") ? "json" : "text";
  }, [content]);

  useEffect(() => {
    let cancelled = false;
    void highlightCode(content, language, DARK_THEME).then((highlighted) => {
      if (!cancelled) setLines(highlighted.lines);
    });
    return () => {
      cancelled = true;
    };
  }, [content, language]);

  return (
    <pre className="overflow-x-auto bg-[#0d1117] p-4 font-mono text-xs leading-relaxed text-[#e4e4e7]">
      {lines
        ? lines.map((line, lineIndex) => (
            // Shiki returns tokens in source order, so the index is the
            // identity here — there is nothing else to key on.
            <div key={lineIndex} className="min-h-[1.2em]">
              {line.tokens.map((token, tokenIndex) => (
                <span key={tokenIndex} style={{ color: token.color }}>
                  {token.content}
                </span>
              ))}
            </div>
          ))
        : content}
    </pre>
  );
}

/**
 * Copy affordance for the sheet's section headers. The icon swaps to a check
 * for a beat: a clipboard write is otherwise completely silent, so without it
 * there is no way to tell a click registered.
 */
function CopyIconButton({
  value,
  label,
  className,
}: {
  value: string;
  label: string;
  className?: string;
}) {
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!copied) return;
    const timer = setTimeout(() => setCopied(false), 1200);
    return () => clearTimeout(timer);
  }, [copied]);

  return (
    <button
      type="button"
      aria-label={copied ? `${label} copied` : `Copy ${label}`}
      className={cn("hover:bg-muted p-1.5", className)}
      onClick={(event) => {
        event.stopPropagation();
        void navigator.clipboard.writeText(value);
        setCopied(true);
      }}
    >
      {copied ? (
        <Check aria-hidden="true" className="text-default-success size-4" />
      ) : (
        <Copy aria-hidden="true" className="size-4" />
      )}
    </button>
  );
}

/** A disclosure for whole sections, as opposed to one body of text. */
function CollapsibleSection({
  title,
  defaultOpen = true,
  children,
}: {
  title: string;
  defaultOpen?: boolean;
  children: React.ReactNode;
}) {
  const [isOpen, setIsOpen] = useState(defaultOpen);
  const contentId = useId();

  return (
    <div className="flex flex-col gap-3">
      <button
        type="button"
        aria-controls={contentId}
        aria-expanded={isOpen}
        onClick={() => setIsOpen((open) => !open)}
        className="group flex min-h-7 items-center gap-2"
      >
        <ChevronRight
          className={cn(
            "text-muted-foreground size-3.5 transition-transform",
            isOpen && "rotate-90",
          )}
        />
        <span className="text-eyebrow">{title}</span>
      </button>
      {isOpen && (
        <div id={contentId} className="flex flex-col gap-5">
          {children}
        </div>
      )}
    </div>
  );
}

function CollapsibleBodySection({
  title,
  content,
  defaultOpen = true,
}: {
  title: string;
  content: string;
  defaultOpen?: boolean;
}) {
  const [isOpen, setIsOpen] = useState(defaultOpen);
  const contentId = useId();

  // Try to pretty-print JSON content
  let displayContent = content;
  try {
    const parsed = JSON.parse(content);
    displayContent = JSON.stringify(parsed, null, 2);
  } catch {
    // Not JSON, use as-is
  }

  return (
    <div className="relative flex flex-col gap-2">
      <button
        type="button"
        aria-controls={contentId}
        aria-expanded={isOpen}
        onClick={() => setIsOpen((open) => !open)}
        className="group flex min-h-7 items-center justify-between pr-12"
      >
        <span className="text-eyebrow">{title}</span>
        <ChevronDown
          aria-hidden="true"
          className={cn(
            "text-muted-foreground absolute top-1.5 right-0 size-4 transition-transform",
            !isOpen && "-rotate-90",
          )}
        />
      </button>
      <CopyIconButton
        value={content}
        label={title}
        className="absolute top-0 right-5 z-10"
      />
      {isOpen && (
        <div
          id={contentId}
          className="border-border max-h-96 overflow-y-auto border"
        >
          <CodeBlock content={displayContent} />
        </div>
      )}
    </div>
  );
}

function MetadataRow({
  label,
  value,
  copyValue,
}: {
  label: string;
  value: string;
  copyValue?: string;
}) {
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!copied) return;
    const timer = setTimeout(() => setCopied(false), 1200);
    return () => clearTimeout(timer);
  }, [copied]);

  return (
    <button
      className="hover:bg-muted/50 flex items-center justify-between gap-4 py-2 text-left transition-colors"
      onClick={() => {
        if (copyValue) {
          void navigator.clipboard.writeText(copyValue);
          setCopied(true);
        }
      }}
      disabled={!copyValue}
      title={copyValue ? `Copy ${label}` : undefined}
    >
      <span className="text-eyebrow shrink-0">{label}</span>
      <span className="flex min-w-0 items-center gap-2">
        {/* The whole row is the button, so the check is the only sign the
            click landed on anything. */}
        {copied && (
          <Check
            aria-hidden="true"
            className="text-default-success size-3.5 shrink-0"
          />
        )}
        <span className="min-w-0 truncate font-mono text-xs">{value}</span>
      </span>
    </button>
  );
}

interface AttributeEntry {
  key: string;
  displayValue: string;
  filterValue: string | null;
}

/**
 * Flatten a nested object into dot-notation keys with filterability metadata.
 * e.g. { http: { request: { method: "POST" } } } =>
 *   [{ key: "http.request.method", displayValue: "POST", filterValue: "POST" }]
 */
function flattenObject(
  obj: Record<string, unknown>,
  prefix = "",
): AttributeEntry[] {
  const result: AttributeEntry[] = [];

  for (const [key, value] of Object.entries(obj)) {
    const fullKey = prefix ? `${prefix}.${key}` : key;

    if (value === null || value === undefined) {
      result.push({ key: fullKey, displayValue: "\u2014", filterValue: null });
      continue;
    }

    switch (typeof value) {
      case "object":
        if (Array.isArray(value)) {
          result.push({
            key: fullKey,
            displayValue: JSON.stringify(value),
            filterValue: null,
          });
        } else if (Object.keys(value).length > 0) {
          result.push(
            ...flattenObject(value as Record<string, unknown>, fullKey),
          );
        }
        break;
      case "string":
        // Empty strings are treated as non-filterable (same as null/undefined):
        // a `key = ""` filter matches empty and missing values equivalently
        // server-side, so surfacing it as a filter option would be misleading.
        result.push({
          key: fullKey,
          displayValue: value || "\u2014",
          filterValue: value || null,
        });
        break;
      case "number":
      case "boolean":
        result.push({
          key: fullKey,
          displayValue: String(value),
          filterValue: String(value),
        });
        break;
      case "bigint":
      case "function":
      case "symbol":
      case "undefined":
      default:
        result.push({
          key: fullKey,
          displayValue: JSON.stringify(value),
          filterValue: JSON.stringify(value),
        });
    }
  }

  return result;
}

function AttributesSection({
  title,
  data,
  onAddFilter,
}: {
  title: string;
  data: Record<string, unknown>;
  onAddFilter?: (path: string, op: Operator, value: string) => void;
}) {
  const flatEntries = flattenObject(data);
  // Controlled state: track which single row's menu is open. Each row has its
  // own DropdownMenu instance, so we need a coordinator to ensure only one is
  // open at a time (Radix's default uncontrolled behavior opened each one
  // independently inside the Sheet modal stack).
  const [openMenuKey, setOpenMenuKey] = useState<string | null>(null);

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between">
        <div className="text-eyebrow">{title}</div>
        <CopyIconButton value={JSON.stringify(data, null, 2)} label={title} />
      </div>
      <div className="border-border divide-border divide-y border-y">
        {flatEntries.map((entry) => {
          const isFilterable = entry.filterValue !== null;

          const rowContent = (
            <>
              <span className="text-muted-foreground shrink-0 text-xs">
                {entry.key}
              </span>
              <span
                className="min-w-0 truncate font-mono text-xs"
                title={entry.displayValue}
              >
                {entry.displayValue}
              </span>
            </>
          );

          if (!onAddFilter) {
            return (
              <div
                key={entry.key}
                className="hover:bg-muted/50 flex items-center justify-between gap-4 py-2 transition-colors"
              >
                {rowContent}
              </div>
            );
          }

          return (
            <DropdownMenu
              key={entry.key}
              open={openMenuKey === entry.key}
              onOpenChange={(open) => setOpenMenuKey(open ? entry.key : null)}
            >
              <DropdownMenuTrigger asChild>
                <button
                  className="hover:bg-muted/50 flex w-full cursor-pointer items-center justify-between gap-4 py-2 text-left transition-colors"
                  aria-label={`Attribute actions for ${entry.key}`}
                >
                  {rowContent}
                </button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="start">
                <DropdownMenuItem
                  disabled={!isFilterable}
                  onClick={() => {
                    // Runtime guard: Radix `disabled` sets aria-disabled but
                    // does not suppress onClick, so re-check filterValue here.
                    if (entry.filterValue !== null) {
                      onAddFilter(entry.key, Operator.Eq, entry.filterValue);
                    }
                  }}
                >
                  <span className="flex items-center gap-1">
                    Filter by
                    <span className="max-w-[200px] truncate font-mono text-xs">
                      {entry.key} = {entry.filterValue ?? ""}
                    </span>
                  </span>
                </DropdownMenuItem>
                <DropdownMenuItem
                  disabled={!isFilterable}
                  onClick={() => {
                    if (entry.filterValue !== null) {
                      onAddFilter(entry.key, Operator.NotEq, entry.filterValue);
                    }
                  }}
                >
                  <span className="flex items-center gap-1">
                    Exclude
                    <span className="max-w-[200px] truncate font-mono text-xs">
                      {entry.key} != {entry.filterValue ?? ""}
                    </span>
                  </span>
                </DropdownMenuItem>
                <DropdownMenuItem
                  disabled={!isFilterable}
                  onClick={() => {
                    if (entry.filterValue !== null) {
                      onAddFilter(
                        entry.key,
                        Operator.Contains,
                        entry.filterValue,
                      );
                    }
                  }}
                >
                  <span className="flex items-center gap-1">
                    Contains
                    <span className="max-w-[200px] truncate font-mono text-xs">
                      {entry.filterValue ?? ""}
                    </span>
                  </span>
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem
                  onClick={() => {
                    void navigator.clipboard.writeText(entry.displayValue);
                  }}
                >
                  Copy value
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          );
        })}
      </div>
    </div>
  );
}
