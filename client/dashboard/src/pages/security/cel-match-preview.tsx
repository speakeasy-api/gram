import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { TextArea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/moonshine";
import { ChevronRight } from "lucide-react";
import { useEffect, useMemo, useState, type JSX, type ReactNode } from "react";
import type { CelEngine, CelMessage, CelSpan } from "./cel-wasm";

const DEBOUNCE_MS = 200;

const KINDS: { value: string; label: string; bodyLabel: string }[] = [
  { value: "user_message", label: "User message", bodyLabel: "Prompt" },
  {
    value: "assistant_message",
    label: "Assistant message",
    bodyLabel: "Assistant reply",
  },
  { value: "tool_request", label: "Tool request", bodyLabel: "" },
  { value: "tool_response", label: "Tool response", bodyLabel: "Tool output" },
];

// Targets recorded against the message body (vs. a tool call's fields). All
// index into the same body text, so their spans highlight the content box.
const BODY_TARGETS = new Set(["content", "prompt", "assistant", "tool_result"]);

const SAMPLE_TOOLS = JSON.stringify(
  [
    {
      name: "shell",
      server: "shell",
      function: "exec",
      args: '{"command":""}',
    },
  ],
  null,
  2,
);

type Verdict =
  | { kind: "idle" }
  | { kind: "match"; spans: CelSpan[] }
  | { kind: "nomatch" }
  | { kind: "error"; message: string };

// Wraps each matched range in a <mark>. The engine's offsets are UTF-8 bytes, so
// we slice in byte space and decode (correct for non-ASCII).
function highlight(text: string, spans: CelSpan[]): ReactNode[] {
  const ranges = spans
    .map((s) => ({ start: s.Start, end: s.End }))
    .filter((r) => r.end > r.start)
    .sort((a, b) => a.start - b.start);
  if (ranges.length === 0) return [text];

  const bytes = new TextEncoder().encode(text);
  const dec = new TextDecoder();
  const nodes: ReactNode[] = [];
  let cursor = 0;
  let key = 0;
  for (const r of ranges) {
    if (r.end <= cursor) continue; // already covered by an overlapping range
    const start = Math.max(r.start, cursor);
    if (start > cursor) nodes.push(dec.decode(bytes.slice(cursor, start)));
    nodes.push(
      <mark key={key++} className="bg-warning/30 text-foreground px-0.5">
        {dec.decode(bytes.slice(start, r.end))}
      </mark>,
    );
    cursor = r.end;
  }
  if (cursor < bytes.length) nodes.push(dec.decode(bytes.slice(cursor)));
  return nodes;
}

function humanizeTarget(target: string): string {
  return target.replace(/^tool\./, "tool ").replace(/\./g, " ");
}

// Evaluates the detection expression against a sample message in-browser and
// shows the verdict plus the matched spans.
export function CelMatchPreview({
  expr,
  engine,
}: {
  expr: string;
  engine: CelEngine;
}): JSX.Element {
  const [open, setOpen] = useState(false);
  const [kind, setKind] = useState("tool_request");
  const [content, setContent] = useState("");
  const [toolsJson, setToolsJson] = useState(SAMPLE_TOOLS);

  const kindMeta = KINDS.find((k) => k.value === kind) ?? KINDS[0]!;
  const isToolRequest = kind === "tool_request";

  // Debounce the inputs so eval doesn't run on every keystroke.
  const [debounced, setDebounced] = useState({
    expr,
    kind,
    content,
    toolsJson,
  });
  useEffect(() => {
    const t = setTimeout(
      () => setDebounced({ expr, kind, content, toolsJson }),
      DEBOUNCE_MS,
    );
    return () => clearTimeout(t);
  }, [expr, kind, content, toolsJson]);

  const verdict = useMemo<Verdict>(() => {
    // Don't evaluate hidden work while the panel is collapsed.
    if (!open) return { kind: "idle" };
    const e = debounced.expr.trim();
    if (!e) return { kind: "idle" };

    let message: CelMessage;
    if (debounced.kind === "tool_request") {
      let tools;
      try {
        tools = JSON.parse(debounced.toolsJson) as CelMessage["tools"];
      } catch {
        return { kind: "error", message: "Tool calls must be valid JSON." };
      }
      message = { type: debounced.kind, content: "", tools };
    } else {
      message = { type: debounced.kind, content: debounced.content };
    }

    let result;
    try {
      result = engine.evalDetection(e, message);
    } catch (err) {
      return {
        kind: "error",
        message: err instanceof Error ? err.message : "evaluation failed",
      };
    }
    if (!result.ok) return { kind: "error", message: result.error };
    return result.matched
      ? { kind: "match", spans: result.spans }
      : { kind: "nomatch" };
  }, [open, debounced, engine]);

  const bodySpans =
    verdict.kind === "match"
      ? verdict.spans.filter((s) => BODY_TARGETS.has(s.Target))
      : [];

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="text-muted-foreground hover:text-foreground flex items-center gap-1 text-xs">
        <ChevronRight
          className={cn("h-3 w-3 transition-transform", open && "rotate-90")}
        />
        Test against a sample message
      </CollapsibleTrigger>
      <CollapsibleContent className="mt-2 space-y-3">
        <div className="flex flex-col gap-2">
          <Select value={kind} onValueChange={setKind}>
            <SelectTrigger
              size="sm"
              aria-label="Sample message type"
              className="w-fit text-xs"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {KINDS.map((k) => (
                <SelectItem key={k.value} value={k.value}>
                  {k.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>

          {isToolRequest ? (
            <label className="space-y-1">
              <span className="text-muted-foreground text-xs">
                Tool calls (JSON)
              </span>
              <TextArea
                value={toolsJson}
                onChange={setToolsJson}
                rows={6}
                className="font-mono text-xs"
              />
            </label>
          ) : (
            <label className="space-y-1">
              <span className="text-muted-foreground text-xs">
                {kindMeta.bodyLabel}
              </span>
              <TextArea value={content} onChange={setContent} rows={3} />
            </label>
          )}
        </div>

        <VerdictView
          verdict={verdict}
          body={isToolRequest ? "" : content}
          bodySpans={bodySpans}
        />
      </CollapsibleContent>
    </Collapsible>
  );
}

function VerdictView({
  verdict,
  body,
  bodySpans,
}: {
  verdict: Verdict;
  body: string;
  bodySpans: CelSpan[];
}): JSX.Element | null {
  if (verdict.kind === "idle") return null;

  if (verdict.kind === "error") {
    return (
      <p className="text-destructive text-xs">
        {verdict.message.replace(/^(?:compile|eval[a-z ]*)\s+"[^"]*":\s*/, "")}
      </p>
    );
  }

  const matched = verdict.kind === "match";
  return (
    <div className="space-y-2">
      <Badge variant={matched ? "success" : "neutral"}>
        {matched ? "Matches" : "No match"}
      </Badge>

      {body && (
        <pre className="bg-input/30 overflow-x-auto p-2 text-xs whitespace-pre-wrap">
          {highlight(body, bodySpans)}
        </pre>
      )}

      {matched && verdict.spans.length > 0 && (
        <ul className="flex flex-wrap gap-1.5">
          {verdict.spans.map((s, i) => (
            <li key={`${s.Target}-${s.Start}-${i}`}>
              <Badge variant="neutral" background={false}>
                <Badge.Text>
                  {humanizeTarget(s.Target)}
                  {s.Path && <span className="opacity-70">.{s.Path}</span>}
                  {": "}
                  <code className="font-mono">{s.Value}</code>
                </Badge.Text>
              </Badge>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
