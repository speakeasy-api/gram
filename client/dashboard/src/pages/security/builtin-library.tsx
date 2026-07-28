import { Button } from "@/components/ui/button";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { useBuiltinExclusions } from "@gram/client/react-query/builtinExclusions.js";
import type { BuiltinExclusionEntry } from "@gram/client/models/components/builtinexclusionentry.js";
import { Info } from "lucide-react";
import type { JSX } from "react";
import { useState } from "react";

const LIBRARY_TOOLTIP =
  "Speakeasy ships a curated preset library that suppresses common false-positive findings — test credentials, documentation examples, and other known-safe values — before they reach your exclusions.";

/**
 * Read-only overview of the built-in exclusion preset library: a title (with an
 * explanatory hover tooltip), a single on/off toggle, and a control to inspect
 * the full catalog. Rendered above the user-managed exclusions table on the
 * Exclusions tab. Engine-internal details (detection sources, rule ids, matcher
 * kinds) are deliberately not surfaced.
 */
export function BuiltinLibrary(): JSX.Element {
  // TODO(track-c): persist builtin_presets.enabled via risk policy update once
  // the API field lands. The generated risk policy get/update client does not
  // yet surface `analyzer_config.builtin_presets.enabled`, so this toggle is
  // wired to local state and defaults to ON to match the intended default.
  const [enabled, setEnabled] = useState<boolean>(true);
  const [libraryOpen, setLibraryOpen] = useState<boolean>(false);

  // Live catalog, fetched from risk.listBuiltinExclusions. Backs the details view.
  const { data, isLoading } = useBuiltinExclusions();
  const categories = data?.categories ?? [];

  return (
    <section className="bg-background mb-6 rounded-xl border p-5">
      <div className="flex items-center justify-between gap-4">
        <SimpleTooltip tooltip={LIBRARY_TOOLTIP}>
          <span className="flex cursor-default items-center gap-1.5">
            <Type className="font-medium">Built-in library</Type>
            <Info className="text-muted-foreground size-3.5" aria-hidden />
          </span>
        </SimpleTooltip>

        {/* The write is protected by the page-level
         *  <RequireScope scope="org:admin"> that wraps PolicyCenter (and thus
         *  ExclusionsTab), the same gate that guards every exclusion mutation. */}
        <Switch
          checked={enabled}
          onCheckedChange={setEnabled}
          aria-label="Enable built-in exclusion library"
        />
      </div>

      <div className="mt-4">
        <Button
          variant="secondary"
          size="sm"
          onClick={() => setLibraryOpen(true)}
          disabled={categories.length === 0}
        >
          {isLoading ? "Loading library…" : "View library"}
        </Button>
      </div>

      <Sheet open={libraryOpen} onOpenChange={setLibraryOpen}>
        <SheetContent className="flex flex-col overflow-y-auto sm:max-w-xl">
          <SheetHeader className="px-6 pt-6">
            <SheetTitle>Built-in exclusion library</SheetTitle>
            <SheetDescription>
              Every value below is published test or documentation data, never a
              real secret. Matching findings are suppressed before they reach
              your exclusions.
            </SheetDescription>
          </SheetHeader>

          <div className="space-y-6 px-6 pb-8">
            {categories.map((category) => (
              <div key={category.label} className="space-y-3">
                <Type className="font-medium">{category.label}</Type>
                <ul className="space-y-3">
                  {category.entries.map((entry) => (
                    <PresetEntryRow key={entry.id} entry={entry} />
                  ))}
                </ul>
              </div>
            ))}
          </div>
        </SheetContent>
      </Sheet>
    </section>
  );
}

function PresetEntryRow({
  entry,
}: {
  entry: BuiltinExclusionEntry;
}): JSX.Element {
  return (
    <li className="bg-muted/30 space-y-1 rounded-lg border p-3">
      <Type small className="font-medium">
        {entry.reason}
      </Type>
      <Type className="text-muted-foreground" small>
        {entry.description}
      </Type>
      {entry.samples && entry.samples.length > 0 && (
        <div className="flex flex-wrap gap-1 pt-1">
          {entry.samples.map((sample) => (
            <code
              key={sample}
              className="bg-background rounded border px-1.5 py-0.5 font-mono text-xs break-all"
            >
              {sample}
            </code>
          ))}
        </div>
      )}
    </li>
  );
}
