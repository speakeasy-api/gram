import { cn } from "@/lib/utils";
import { useRef, useState, type JSX, type KeyboardEvent } from "react";
import { matchQuerySuggestions, type QuerySuggestion } from "./match-query";

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
      {open && (
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
          <div className="text-muted-foreground/70 border-border mt-1 border-t px-3 pt-1 text-[10px] leading-relaxed">
            <code>field:val</code> is · <code>*val*</code> contains ·{" "}
            <code>val*</code> starts · <code>/re/</code> regex ·{" "}
            <code>(a OR b)</code> any · <code>-field:</code> not ·{" "}
            <code>field:*</code> exists
          </div>
        </div>
      )}
      {error && <p className="text-destructive mt-1 text-xs">{error}</p>}
    </div>
  );
}
