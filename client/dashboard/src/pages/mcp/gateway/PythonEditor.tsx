import { DARK_THEME } from "@/components/ui/lib/codeUtils";
import { cn } from "@/lib/utils";
import {
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import type { HighlighterGeneric, ThemedToken } from "shiki";

type Highlighter = HighlighterGeneric<"python", typeof DARK_THEME>;

let highlighterLoad: Promise<Highlighter> | undefined;

/** One shared highlighter, so typing tokenizes synchronously. */
function loadHighlighter(): Promise<Highlighter> {
  highlighterLoad ??= import("shiki").then(
    ({ getSingletonHighlighter }) =>
      getSingletonHighlighter({
        themes: [DARK_THEME],
        langs: ["python"],
      }) as Promise<Highlighter>,
  );
  return highlighterLoad;
}

const INDENT = "    ";

// The textarea and the highlighted copy under it must lay text out the same.
const TEXT_LAYOUT =
  "col-start-1 row-start-1 m-0 min-h-48 w-full px-4 py-3 font-mono text-[13px] leading-5 break-words whitespace-pre-wrap [tab-size:4]";

/**
 * A Python editor: a plain textarea over a syntax-highlighted copy of its
 * text, always dark. Tab indents; Escape, then Tab, leaves the editor.
 * Ctrl/⌘ Enter calls `onSubmit`.
 */
export function PythonEditor({
  value,
  onChange,
  onSubmit,
  disabled,
  label,
  describedBy,
  actions,
}: {
  value: string;
  onChange: (value: string) => void;
  onSubmit: () => void;
  disabled?: boolean;
  label: string;
  /** IDs of text that explains the editor, e.g. the run hint. */
  describedBy?: string;
  /** Controls on the right of the editor's header bar. */
  actions?: ReactNode;
}): JSX.Element {
  const keysId = useId();
  const textarea = useRef<HTMLTextAreaElement>(null);
  // Set by Escape: the next Tab moves focus instead of indenting.
  const releaseTab = useRef(false);
  const [highlighter, setHighlighter] = useState<Highlighter>();
  useEffect(() => {
    let live = true;
    loadHighlighter().then(
      (loaded) => live && setHighlighter(loaded),
      () => undefined, // Stays plain text.
    );
    return () => {
      live = false;
    };
  }, []);

  const lines = useMemo<ThemedToken[][] | undefined>(() => {
    if (!highlighter) return undefined;
    try {
      return highlighter.codeToTokens(value, {
        lang: "python",
        theme: DARK_THEME,
      }).tokens;
    } catch {
      return undefined;
    }
  }, [highlighter, value]);

  const insert = (text: string) => {
    const element = textarea.current;
    if (!element) return;
    element.focus();
    // execCommand keeps the browser's undo history; fall back without it.
    if (document.execCommand?.("insertText", false, text)) return;
    const { selectionStart, selectionEnd } = element;
    const next =
      value.slice(0, selectionStart) + text + value.slice(selectionEnd);
    onChange(next);
    requestAnimationFrame(() => {
      element.selectionStart = element.selectionEnd =
        selectionStart + text.length;
    });
  };

  return (
    <div
      className={cn(
        "border-border bg-surface-secondary-fixed-dark focus-within:border-foreground/50 flex flex-col border transition-colors",
        disabled && "opacity-70",
      )}
    >
      <div className="bg-surface-tertiary-fixed-dark flex min-h-8 items-center justify-between gap-3 px-4 py-1">
        <span className="text-muted-fixed-light font-mono text-[11px]">
          Python
        </span>
        {actions}
      </div>
      <div className="grid max-h-[32rem] overflow-auto">
        <pre
          aria-hidden
          className={cn(TEXT_LAYOUT, "text-default-fixed-light select-none")}
        >
          {lines
            ? lines.map((line, index) => (
                <span key={index}>
                  {line.map((token, at) => (
                    <span key={at} style={{ color: token.color }}>
                      {token.content}
                    </span>
                  ))}
                  {"\n"}
                </span>
              ))
            : value}
          {/* Holds the height of a trailing empty line. */}{" "}
        </pre>
        <textarea
          ref={textarea}
          aria-label={label}
          aria-describedby={[keysId, describedBy].filter(Boolean).join(" ")}
          value={value}
          onChange={(event) => onChange(event.target.value)}
          disabled={disabled}
          spellCheck={false}
          autoCapitalize="off"
          autoComplete="off"
          autoCorrect="off"
          onKeyDown={(event) => {
            if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
              event.preventDefault();
              onSubmit();
              return;
            }
            if (event.key === "Escape") {
              releaseTab.current = true;
              return;
            }
            if (event.key === "Tab" && !event.shiftKey) {
              if (releaseTab.current) {
                releaseTab.current = false;
                return;
              }
              event.preventDefault();
              insert(INDENT);
              return;
            }
            releaseTab.current = false;
            if (event.key === "Enter" && !event.shiftKey) {
              // Keep the line's indent, one level deeper after a colon.
              const start = event.currentTarget.selectionStart;
              const line = value.slice(
                value.lastIndexOf("\n", start - 1) + 1,
                start,
              );
              const indent = /^[ \t]*/.exec(line)?.[0] ?? "";
              const deeper = line.trimEnd().endsWith(":") ? INDENT : "";
              if (!indent && !deeper) return;
              event.preventDefault();
              insert(`\n${indent}${deeper}`);
            }
          }}
          className={cn(
            TEXT_LAYOUT,
            "selection:bg-foreground/30 resize-none overflow-hidden border-0 bg-transparent text-transparent [caret-color:var(--text-default-fixed-light)] outline-none disabled:cursor-not-allowed",
          )}
        />
      </div>
      <span id={keysId} className="sr-only">
        Ctrl or Command Enter executes the code. Tab indents; press Escape, then
        Tab, to leave the editor.
      </span>
    </div>
  );
}
