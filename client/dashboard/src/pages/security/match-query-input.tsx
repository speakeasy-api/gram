import { cn } from "@/lib/utils";
import { ChevronRight } from "lucide-react";
import {
  Fragment,
  useRef,
  useState,
  type JSX,
  type KeyboardEvent,
} from "react";
import {
  MATCH_QUERY_EXAMPLES,
  matchQuerySuggestions,
  type QuerySuggestion,
} from "./match-query";

/** A Datadog-style single-line query input for a rule's match_config, with a
 *  context-aware autocomplete popover. Controlled on the raw query string; the
 *  form parses it into conditions. ↓ enters the dropdown; ↑/↓ move, Enter/Tab
 *  accept, Esc closes. */
export function MatchQueryInput({
  value,
  onChange,
  error,
}: {
  value: string;
  onChange: (next: string) => void;
  error?: string | null;
}): JSX.Element {
  const inputRef = useRef<HTMLInputElement>(null);
  const [open, setOpen] = useState(false);
  const [caret, setCaret] = useState(value.length);
  const [active, setActive] = useState(-1);

  const { from, suggestions } = matchQuerySuggestions(value, caret);

  const applySuggestion = (s: QuerySuggestion) => {
    const next = value.slice(0, from) + s.insert + value.slice(caret);
    onChange(next);
    const newCaret = from + (s.caretOffset ?? s.insert.length);
    setActive(-1);
    requestAnimationFrame(() => {
      const el = inputRef.current;
      if (!el) return;
      el.focus();
      el.setSelectionRange(newCaret, newCaret);
      setCaret(newCaret);
    });
  };

  const syncCaret = (el: HTMLInputElement) => {
    setCaret(el.selectionStart ?? el.value.length);
    setActive(-1);
  };

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (!open || suggestions.length === 0) {
      if (e.key === "ArrowDown") setOpen(true);
      return;
    }
    switch (e.key) {
      case "ArrowDown":
        e.preventDefault();
        setActive((i) => (i + 1) % suggestions.length);
        break;
      case "ArrowUp":
        e.preventDefault();
        setActive((i) => (i <= 0 ? suggestions.length - 1 : i - 1));
        break;
      case "Enter":
      case "Tab": {
        const pick = suggestions[active] ?? suggestions[0];
        if (pick) {
          e.preventDefault();
          applySuggestion(pick);
        }
        break;
      }
      case "Escape":
        e.preventDefault();
        setOpen(false);
        break;
    }
  };

  return (
    <div className="relative">
      <input
        ref={inputRef}
        value={value}
        spellCheck={false}
        autoComplete="off"
        placeholder="tool_call.name:bash AND tool_call.args:(*rm* OR *curl*)"
        onChange={(e) => {
          onChange(e.target.value);
          syncCaret(e.target);
          setOpen(true);
        }}
        onKeyUp={(e) => {
          if (
            e.key !== "ArrowDown" &&
            e.key !== "ArrowUp" &&
            e.key !== "Enter"
          ) {
            syncCaret(e.currentTarget);
          }
        }}
        onKeyDown={onKeyDown}
        onClick={(e) => syncCaret(e.currentTarget)}
        onFocus={() => setOpen(true)}
        onBlur={() => {
          window.setTimeout(() => setOpen(false), 150);
        }}
        className={cn(
          "border-input bg-background ring-offset-background focus-visible:ring-ring h-10 w-full rounded-md border px-3 py-2 font-mono text-xs focus-visible:ring-2 focus-visible:outline-none",
          error && "border-destructive",
        )}
      />
      {open && suggestions.length > 0 && (
        <div className="border-border bg-popover absolute z-50 mt-1 max-h-64 w-full overflow-y-auto rounded-md border py-1 shadow-md">
          {suggestions.map((s, i) => (
            <button
              key={`${s.label}-${s.insert}`}
              type="button"
              onMouseEnter={() => setActive(i)}
              onMouseDown={(e) => {
                e.preventDefault();
                applySuggestion(s);
              }}
              className={cn(
                "flex w-full items-center gap-3 px-3 py-1.5 text-left",
                i === active ? "bg-muted" : "hover:bg-muted/60",
              )}
            >
              <span className="font-mono text-xs">{s.label}</span>
              <span className="text-muted-foreground flex-1 truncate text-[11px]">
                {s.description}
              </span>
              {s.group && (
                <span className="text-muted-foreground/70 shrink-0 text-[10px] uppercase">
                  {s.group}
                </span>
              )}
            </button>
          ))}
        </div>
      )}
      {error && <p className="text-destructive mt-1 text-xs">{error}</p>}
      <MatchQueryHelp />
    </div>
  );
}

/** Worked-example syntax reference, collapsed by default. */
function MatchQueryHelp(): JSX.Element {
  return (
    <details className="group mt-2">
      <summary className="text-muted-foreground hover:text-foreground flex cursor-pointer list-none items-center gap-1 text-xs">
        <ChevronRight className="h-3 w-3 transition-transform group-open:rotate-90" />
        Query syntax & examples
      </summary>
      <div className="border-border bg-muted/30 mt-1.5 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 rounded-md border p-3">
        {MATCH_QUERY_EXAMPLES.map((ex) => (
          <Fragment key={ex.query}>
            <code className="text-foreground font-mono text-[11px]">
              {ex.query}
            </code>
            <span className="text-muted-foreground text-[11px]">
              {ex.meaning}
            </span>
          </Fragment>
        ))}
      </div>
    </details>
  );
}
