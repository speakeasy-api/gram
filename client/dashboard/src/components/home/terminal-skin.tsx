import { useEffect, useState, type FC, type PropsWithChildren } from "react";
import {
  ThreadPrimitive,
  useAuiState,
  type ToolCallMessagePartComponent,
} from "@assistant-ui/react";
import { THINKING_VERBS } from "@/elements/lib/thinkingVerbs";
import { ToolFallback } from "@/elements/components/assistant-ui/tool-fallback";
import { useElements } from "@/elements/hooks/useElements";
import { useToolApproval } from "@/elements/hooks/useToolApproval";
import {
  displayToolName,
  formatArgs,
  isToolError,
  resultText,
} from "./terminal-format";

// A one-line result up to this long shows in place of its summary.
const SUMMARY_CHARS = 120;

/**
 * One tool call as a terminal line:
 *
 *   ⏺ list_servers(project: "default")
 *     ⎿  12 lines (click to expand)
 */
export const TerminalToolCall: ToolCallMessagePartComponent = (props) => {
  const { pendingApprovals } = useToolApproval();
  const [expanded, setExpanded] = useState(false);

  // Approvals need the stock card's buttons.
  if (pendingApprovals.has(props.toolCallId)) {
    return <ToolFallback {...props} />;
  }

  const { toolName, args, result, status } = props;
  const text = resultText(result).trim();
  const isError = isToolError(status.type, result);
  const state =
    status.type === "running" ? "running" : isError ? "error" : "done";

  const lines = text ? text.split("\n") : [];
  // Collapsed by default, like Claude Code: one summary line under the call.
  // A short one-line result is its own summary; an error leads with its first
  // line so the failure is readable without expanding.
  const short = lines.length === 1 && text.length <= SUMMARY_CHARS;
  const summary = short
    ? text
    : isError && lines[0]
      ? lines[0]
      : lines.length > 0
        ? `${lines.length} ${lines.length === 1 ? "line" : "lines"}`
        : isError
          ? "Failed"
          : "Done";
  const tool = displayToolName(toolName);

  return (
    <div className="gram-term-tool" data-state={state}>
      <div className="gram-term-tool-call">
        <span className="gram-term-bullet" aria-hidden="true">
          ⏺
        </span>
        <span className="gram-term-tool-name">{tool.name}</span>
        {tool.mcp && <span className="gram-term-dim"> (MCP)</span>}
        <span className="gram-term-tool-args">({formatArgs(args)})</span>
      </div>
      {state === "running" ? (
        <div className="gram-term-tool-result">
          <span aria-hidden="true">⎿</span>
          <span>Running…</span>
        </div>
      ) : (
        <div className="gram-term-tool-result">
          <span aria-hidden="true">⎿</span>
          <div className={expanded ? "min-w-0" : "flex min-w-0"}>
            {expanded ? (
              <pre data-expanded>{text}</pre>
            ) : (
              <span className="min-w-0 truncate">{summary}</span>
            )}
            {!short && text && (
              <button
                type="button"
                className="gram-term-expand shrink-0 whitespace-pre"
                onClick={() => setExpanded((v) => !v)}
              >
                {expanded ? "Show less" : " (click to expand)"}
              </button>
            )}
          </div>
        </div>
      )}
    </div>
  );
};

/**
 * Tool runs render flat, one line per call, like a terminal transcript. While
 * the turn is still working after the last call, a "✻ verb…" line follows —
 * the stock group shows that state with a shimmering heading instead.
 */
export const TerminalToolGroup: FC<
  PropsWithChildren<{ indices: number[] }>
> = ({ children, indices }) => {
  const working = useAuiState(({ message }) => {
    if (message.status?.type !== "running") return false;
    const last = indices[indices.length - 1] ?? -1;
    return message.parts
      .slice(last + 1)
      .every((part) => part.type === "text" && part.text.trim() === "");
  });
  return (
    <div className="gram-term-tools">
      {children}
      {working && <TerminalWorking />}
    </div>
  );
};

const VERB_MS = 3000;

function TerminalWorking(): JSX.Element {
  const [index, setIndex] = useState(() =>
    Math.floor(Math.random() * THINKING_VERBS.length),
  );
  useEffect(() => {
    const id = setInterval(
      () => setIndex((i) => (i + 1) % THINKING_VERBS.length),
      VERB_MS,
    );
    return () => clearInterval(id);
  }, []);
  return (
    <div className="aui-thinking gram-term-working" role="status">
      <span className="aui-thinking-label">{THINKING_VERBS[index]}…</span>
    </div>
  );
}

export const TerminalWelcome: FC = () => {
  const { config } = useElements();
  const { title, subtitle, suggestions = [] } = config.welcome ?? {};
  return (
    <div className="gram-term-welcome">
      <div className="gram-term-welcome-box">
        <p>
          <span className="gram-term-accent">✻</span> {title}
        </p>
        {subtitle && <p className="gram-term-dim">{subtitle}</p>}
        <p className="gram-term-dim">/ for saved prompts</p>
      </div>
      {suggestions.length > 0 && (
        <div className="gram-term-tips">
          <p className="gram-term-dim">Try:</p>
          <ol>
            {suggestions.slice(0, 4).map((s) => (
              <li key={s.title}>
                <ThreadPrimitive.Suggestion prompt={s.prompt} send asChild>
                  <button type="button">{s.title}</button>
                </ThreadPrimitive.Suggestion>
              </li>
            ))}
          </ol>
        </div>
      )}
    </div>
  );
};
