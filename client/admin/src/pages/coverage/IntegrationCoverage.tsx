import { useRef, useState, type JSX } from "react";
import { Grid2X2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { errorMessage } from "@/lib/gramAdminApi";
import { supportMatrixQuery, saveSupportMatrix } from "./api";
import { CatalogContext } from "./catalogContext";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { MethodEditor, MethodList } from "./CoverageEditor";
import { MatrixExplorer, type Selection } from "./MatrixExplorer";
import { ImportCsvDialog } from "./ImportCsvDialog";
import { draftSchema, storageKey, type Draft, type Snapshot } from "./model";

function readBrowserDraft(): Draft | null {
  try {
    const raw = localStorage.getItem(storageKey);
    return raw ? draftSchema.parse(JSON.parse(raw)) : null;
  } catch {
    return null;
  }
}

export function IntegrationCoverage(): JSX.Element {
  const query = useQuery(supportMatrixQuery);
  if (query.isPending) return <p role="status">Loading support matrix…</p>;
  if (!query.data)
    return (
      <div className="space-y-3">
        <h1 className="text-2xl font-semibold">Support matrix</h1>
        <p role="alert">{errorMessage(query.error)}</p>
        <Button onClick={() => void query.refetch()}>Retry</Button>
      </div>
    );
  return (
    <CatalogContext.Provider value={query.data}>
      <SupportMatrixPage snapshot={query.data} />
    </CatalogContext.Provider>
  );
}

function SupportMatrixPage({ snapshot }: { snapshot: Snapshot }): JSX.Element {
  const { methods, products, capabilities, draft } = snapshot;
  const queryClient = useQueryClient();
  const [legacy, setLegacy] = useState(readBrowserDraft);
  const [error, setError] = useState("");
  const [selection, setSelection] = useState<Selection | null>(null);
  const saving = useRef(false);
  const mutation = useMutation({
    mutationFn: (next: Draft) => saveSupportMatrix(snapshot.revision, next),
    onSuccess: (next) =>
      queryClient.setQueryData(supportMatrixQuery.queryKey, next),
  });
  async function save(next: Draft, propagateError = false): Promise<boolean> {
    if (saving.current) {
      if (propagateError)
        throw new Error("Another save is in progress. Try importing again.");
      return false;
    }
    saving.current = true;
    setError("");
    try {
      await mutation.mutateAsync(next);
      return true;
    } catch (err) {
      setError(errorMessage(err));
      if (propagateError) throw err;
      return false;
    } finally {
      saving.current = false;
    }
  }
  function clearLegacy() {
    try {
      localStorage.removeItem(storageKey);
      setLegacy(null);
      setError("");
    } catch {
      setError(
        "Could not clear the saved browser draft. Check browser storage permissions and try again.",
      );
    }
  }
  async function importLegacy() {
    if (!legacy) return;
    const merged: Draft = {
      mappings: { ...draft.mappings },
      references: { ...draft.references },
      accounts: { ...draft.accounts, ...legacy.accounts },
    };
    for (const [key, mapping] of Object.entries(legacy.mappings))
      merged.mappings[key] = {
        ...mapping,
        facts: { ...draft.mappings[key]?.facts, ...mapping.facts },
      };
    for (const [key, facts] of Object.entries(legacy.references))
      merged.references[key] = { ...draft.references[key], ...facts };
    if (await save(merged)) {
      setLegacy(null);
      try {
        localStorage.removeItem(storageKey);
      } catch {
        /* Database save succeeded; the browser copy remains available. */
      }
    }
  }
  const mappedCount = Object.values(draft.mappings).filter(
    (mapping) => mapping.applicability !== "unknown",
  ).length;
  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <div className="mb-1 flex items-center gap-2">
            <Grid2X2 className="size-5" />
            <Badge variant="outline">Shared catalog</Badge>
          </div>
          <h1 className="text-2xl font-semibold tracking-tight">
            Support matrix
          </h1>
          <p className="text-muted-foreground mt-1 max-w-2xl text-sm">
            Explore what each integration enables, and where. Select any
            capability to inspect or edit its coverage.
          </p>
        </div>
        <div className="text-muted-foreground text-right text-xs leading-5">
          <p>
            {methods.length} methods · {products.length} products ·{" "}
            {capabilities.length} capabilities
          </p>
          <p>
            {mappedCount} / {methods.length * products.length} product mappings
            assessed
          </p>
          <p role="status">
            {mutation.isPending
              ? "Saving to database…"
              : "Saved in the shared database"}
          </p>
          <ImportCsvDialog
            revision={snapshot.revision}
            catalog={snapshot}
            draft={draft}
            disabled={mutation.isPending}
            onImport={async (next) => {
              await save(next, true);
            }}
          />
          <Button
            variant="ghost"
            size="sm"
            disabled={mutation.isPending}
            onClick={() => {
              setSelection(null);
              setError("");
              void queryClient.invalidateQueries({
                queryKey: supportMatrixQuery.queryKey,
              });
            }}
          >
            Reload matrix
          </Button>
        </div>
      </header>
      {error && (
        <p role="alert" className="text-destructive text-sm">
          {error}
        </p>
      )}
      {legacy && (
        <div className="space-y-2 rounded-lg border p-4">
          <p className="text-sm">
            A browser draft is available ({Object.keys(legacy.mappings).length}{" "}
            mappings). Importing merges it into the database and replaces
            overlapping values with your browser edits.
          </p>
          <Button
            variant="outline"
            disabled={mutation.isPending}
            onClick={() => void importLegacy()}
          >
            Import browser draft
          </Button>
          <Button
            variant="ghost"
            disabled={mutation.isPending}
            onClick={clearLegacy}
          >
            Clear
          </Button>
        </div>
      )}
      <fieldset disabled={mutation.isPending} className="contents">
        <MatrixExplorer draft={draft} onSave={save} onSelect={setSelection} />
      </fieldset>
      <Sheet
        open={!!selection}
        onOpenChange={(open) => {
          if (!open) setSelection(null);
        }}
      >
        <SheetContent className="w-full overflow-y-auto sm:max-w-xl">
          {error && (
            <p role="alert" className="text-destructive px-4 pt-4 text-sm">
              {error}
            </p>
          )}
          <SheetHeader>
            <SheetTitle>{selection?.capability.name}</SheetTitle>
            <SheetDescription>
              {selection?.product?.name ?? selection?.method?.name} ·{" "}
              {selection?.capability.group}
            </SheetDescription>
          </SheetHeader>
          {selection && (
            <fieldset
              disabled={mutation.isPending}
              className="space-y-4 px-4 pb-6"
            >
              <p className="text-muted-foreground text-sm">
                {selection.product
                  ? "Inspect each integration method below. Save applicability and capability coverage for this product."
                  : "This method-level claim applies to supported platforms unless a platform condition or explicit coverage entry overrides it."}
              </p>
              {selection.method && (
                <MethodEditor
                  key={`${selection.method.id}/${selection.product?.id ?? "reference"}/${selection.capability.id}`}
                  method={selection.method}
                  product={selection.product}
                  capability={selection.capability}
                  draft={draft}
                  onSave={save}
                />
              )}
              {!selection.method && selection.product && (
                <MethodList
                  methods={methods}
                  account={selection.account}
                  product={selection.product}
                  capability={selection.capability}
                  draft={draft}
                  onSave={save}
                />
              )}
            </fieldset>
          )}
        </SheetContent>
      </Sheet>
    </div>
  );
}
