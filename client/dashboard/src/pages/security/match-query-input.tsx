import { cn } from "@/lib/utils";
import { useRef, useState, type JSX } from "react";
import { matchQuerySuggestions, type QuerySuggestion } from "./match-query";

/** A Datadog-style single-line query input for a rule's match_config, with an
 *  autocomplete popover suggesting fields, operators, and AND/OR connectors.
 *  Controlled on the raw query string; the form parses it into conditions. */
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

  const { from, suggestions } = matchQuerySuggestions(value, caret);

  const applySuggestion = (s: QuerySuggestion) => {
    const next = value.slice(0, from) + s.insert + value.slice(caret);
    onChange(next);
    const newCaret = from + s.insert.length;
    requestAnimationFrame(() => {
      const el = inputRef.current;
      if (!el) return;
      el.focus();
      el.setSelectionRange(newCaret, newCaret);
      setCaret(newCaret);
    });
  };

  const syncCaret = (el: HTMLInputElement) =>
    setCaret(el.selectionStart ?? el.value.length);

  return (
    <div className="relative">
      <input
        ref={inputRef}
        value={value}
        spellCheck={false}
        autoComplete="off"
        placeholder="tool_server is mise AND tool_args.$.scope is all"
        onChange={(e) => {
          onChange(e.target.value);
          syncCaret(e.target);
        }}
        onKeyUp={(e) => syncCaret(e.currentTarget)}
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
        <div className="border-border bg-popover absolute z-50 mt-1 max-h-64 w-full overflow-y-auto rounded-md border shadow-md">
          {suggestions.map((s) => (
            <button
              key={`${s.label}-${s.insert}`}
              type="button"
              onMouseDown={(e) => {
                e.preventDefault();
                applySuggestion(s);
              }}
              className="hover:bg-muted/60 flex w-full flex-col items-start gap-0.5 px-3 py-1.5 text-left"
            >
              <span className="font-mono text-xs">{s.label}</span>
              <span className="text-muted-foreground text-[11px]">
                {s.description}
              </span>
            </button>
          ))}
        </div>
      )}
      {error && <p className="text-destructive mt-1 text-xs">{error}</p>}
    </div>
  );
}
