import { Button } from "@/components/ui/Button";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { Text } from "@/components/ui/Text";
import { useBuiltinExclusions } from "@gram/client/react-query/builtinExclusions.js";
import type { BuiltinExclusionEntry } from "@gram/client/models/components/builtinexclusionentry.js";
import { Info } from "lucide-react";
import type { JSX } from "react";
import { useState } from "react";

const LIBRARY_TOOLTIP =
  "Speakeasy ships a curated preset library that suppresses common false-positive findings — test credentials, documentation examples, and other known-safe values — before they reach your exclusions.";

/**
 * Read-only overview of the built-in exclusion preset library: a title (with an
 * explanatory hover tooltip) and a control to inspect the full catalog.
 * Rendered above the user-managed exclusions table on the Exclusions tab.
 * Engine-internal details (detection sources, rule ids, matcher kinds) are
 * deliberately not surfaced.
 */
export function BuiltinLibrary(): JSX.Element {
  const [libraryOpen, setLibraryOpen] = useState<boolean>(false);

  // Live catalog, fetched from risk.listBuiltinExclusions. Backs the details view.
  const { data, isLoading } = useBuiltinExclusions();
  const categories = data?.categories ?? [];

  return (
    <section className="bg-background mb-6 border p-5">
      <div className="flex items-center justify-between gap-4">
        <SimpleTooltip tooltip={LIBRARY_TOOLTIP}>
          <span className="flex cursor-default items-center gap-1.5">
            <Text className="font-medium">Built-in library</Text>
            <Info className="text-muted-foreground size-3.5" aria-hidden />
          </span>
        </SimpleTooltip>
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
                <Text className="font-medium">{category.label}</Text>
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
    <li className="bg-muted/30 space-y-1 border p-3">
      <Text small className="font-medium">
        {entry.reason}
      </Text>
      <Text className="text-muted-foreground" small>
        {entry.description}
      </Text>
      {entry.samples && entry.samples.length > 0 && (
        <div className="flex flex-wrap gap-1 pt-1">
          {entry.samples.map((sample) => (
            <code
              key={sample}
              className="bg-background border px-1.5 py-0.5 font-mono text-xs break-all"
            >
              {sample}
            </code>
          ))}
        </div>
      )}
    </li>
  );
}
