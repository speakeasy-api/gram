import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { BookOpen } from "lucide-react";
import { useState, type JSX } from "react";
import { useCelEngine } from "./use-cel-engine";

// One reference for the whole editing surface: fields, matchers, and macros
// from the engine catalog, in a side sheet with aligned term/description
// columns. Renders nothing while the engine is loading.
export function CelReferenceSheet(): JSX.Element | null {
  const [open, setOpen] = useState(false);
  const engine = useCelEngine();
  if (engine.status !== "ready") return null;
  const reference = engine.engine.reference;

  return (
    <Sheet open={open} onOpenChange={setOpen}>
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="text-muted-foreground hover:text-foreground flex shrink-0 items-center gap-1 text-xs underline-offset-2 hover:underline"
      >
        <BookOpen className="h-3.5 w-3.5" />
        Fields &amp; matchers
      </button>
      <SheetContent className="w-full overflow-y-auto sm:max-w-xl">
        <SheetHeader>
          <SheetTitle>Fields &amp; matchers</SheetTitle>
          <SheetDescription>
            Everything an expression can reference. The same catalog applies to
            every scope and detection field.
          </SheetDescription>
        </SheetHeader>
        <div className="space-y-6 px-4 pb-6">
          <ReferenceSection
            title="Fields"
            rows={(reference.variables ?? []).map((v) => ({
              term: v.name,
              hint: v.type,
              note: v.description,
            }))}
          />
          <ReferenceSection
            title="Matchers"
            rows={(reference.matchers ?? []).map((f) => ({
              term: compactSignature(f.signature),
              hint: returnHint(f.signature),
              note: f.description,
            }))}
          />
          <ReferenceSection
            title="Macros"
            rows={(reference.macros ?? []).map((m) => ({
              term: compactSignature(m.signature),
              hint: returnHint(m.signature),
              note: m.description,
            }))}
          />
        </div>
      </SheetContent>
    </Sheet>
  );
}

// "field.matchGlob(pattern: string) -> bool" reads as reference-manual syntax;
// show "matchGlob(pattern)" and put the return type in a muted hint instead.
function compactSignature(signature: string): string {
  return signature
    .replace(/\s*->.*$/, "")
    .replace(/^(?:field|list)\./, "")
    .replace(/\(([^)]*)\)/, (_m, args: string) => {
      const names = args
        .split(",")
        .map((a) => a.split(":")[0]?.trim() ?? "")
        .filter(Boolean);
      return `(${names.join(", ")})`;
    });
}

function returnHint(signature: string): string {
  const match = /->\s*(.+)$/.exec(signature);
  const ret = match?.[1]?.trim() ?? "";
  // bool is the expected default for predicates; only surface the exceptions.
  return ret === "" || ret === "bool" ? "" : `→ ${ret}`;
}

function ReferenceSection({
  title,
  rows,
}: {
  title: string;
  rows: { term: string; hint: string; note: string }[];
}): JSX.Element | null {
  if (rows.length === 0) return null;
  return (
    <section className="space-y-2">
      <h3 className="text-muted-foreground text-xs font-medium tracking-wide uppercase">
        {title}
      </h3>
      <div className="grid grid-cols-[minmax(8rem,14rem)_minmax(0,1fr)] gap-x-4 gap-y-2">
        {rows.map((row) => (
          <ReferenceRow key={row.term} row={row} />
        ))}
      </div>
    </section>
  );
}

function ReferenceRow({
  row,
}: {
  row: { term: string; hint: string; note: string };
}): JSX.Element {
  return (
    <>
      <div className="min-w-0">
        <code className="text-foreground font-mono text-xs break-all">
          {row.term}
        </code>
        {row.hint && (
          <span className="text-muted-foreground ml-1 text-[11px] whitespace-nowrap">
            {row.hint}
          </span>
        )}
      </div>
      <p className="text-muted-foreground min-w-0 text-xs">{row.note}</p>
    </>
  );
}
