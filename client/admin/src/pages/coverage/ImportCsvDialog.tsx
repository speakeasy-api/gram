import { useRef, useState, type JSX } from "react";
import { Copy, Upload } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { errorMessage } from "@/lib/gramAdminApi";
import {
  importPrompt,
  maxImportBytes,
  parseMatrixImport,
  type CsvImport,
  type ImportCatalog,
} from "./importCsv";
import type { Draft } from "./model";
import { serializeSupportMatrixUpdate } from "./request";

export function ImportCsvDialog({
  catalog,
  draft,
  revision,
  onImport,
  disabled = false,
}: {
  catalog: ImportCatalog;
  draft: Draft;
  revision: string;
  onImport: (draft: Draft) => Promise<void>;
  disabled?: boolean;
}): JSX.Element {
  const [open, setOpen] = useState(false);
  const [csv, setCsv] = useState<string | null>(null);
  const [reading, setReading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const readVersion = useRef(0);
  const prompt = importPrompt(catalog);
  let parsed: CsvImport | null = null;
  let validationError = "";
  if (csv !== null) {
    try {
      const candidate = parseMatrixImport(csv, catalog, draft);
      serializeSupportMatrixUpdate(revision, candidate.draft);
      parsed = candidate;
    } catch (err) {
      validationError = errorMessage(err);
    }
  }
  const busy = saving || reading;
  const message = error || validationError;

  async function copyPrompt() {
    try {
      await navigator.clipboard.writeText(prompt);
      toast.success("Prompt copied");
    } catch {
      setError("Could not copy. Select and copy the prompt below.");
    }
  }

  async function readFile(file: File | undefined) {
    const version = ++readVersion.current;
    setCsv(null);
    setError("");
    setReading(false);
    if (!file) return;
    if (file.size > maxImportBytes) {
      setError("CSV must be 2 MB or smaller.");
      return;
    }
    setReading(true);
    try {
      const text = await file.text();
      if (version === readVersion.current) setCsv(text);
    } catch {
      if (version === readVersion.current)
        setError("Could not read this file. Choose it again.");
    } finally {
      if (version === readVersion.current) setReading(false);
    }
  }

  async function submit() {
    if (!parsed) return;
    setSaving(true);
    setError("");
    try {
      await onImport(parsed.draft);
      toast.success("Support matrix imported");
      setOpen(false);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setSaving(false);
    }
  }

  function changeOpen(next: boolean) {
    if (saving) return;
    readVersion.current++;
    setOpen(next);
    setCsv(null);
    setReading(false);
    setError("");
  }

  return (
    <>
      <Button
        variant="outline"
        disabled={disabled}
        onClick={() => changeOpen(true)}
      >
        <Upload className="size-4" /> Import CSV
      </Button>
      <Dialog open={open} onOpenChange={changeOpen}>
        <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>Import support matrix</DialogTitle>
            <DialogDescription>
              Give your agent this prompt and your source matrix, then upload
              the CSV it produces.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-3">
            <div className="flex items-center justify-between gap-3">
              <label
                htmlFor="matrix-import-prompt"
                className="text-sm font-medium"
              >
                1. Format with your agent
              </label>
              <Button
                variant="outline"
                size="sm"
                onClick={() => {
                  void copyPrompt();
                }}
              >
                <Copy className="size-4" /> Copy prompt
              </Button>
            </div>
            <textarea
              id="matrix-import-prompt"
              readOnly
              value={prompt}
              className="border-input bg-muted text-muted-foreground h-48 w-full rounded-md border p-3 font-mono text-xs"
            />
            <label
              htmlFor="matrix-import-file"
              className="block text-sm font-medium"
            >
              2. Upload the formatted CSV
            </label>
            <Input
              id="matrix-import-file"
              type="file"
              accept=".csv,text/csv"
              disabled={saving}
              onChange={(event) => {
                void readFile(event.target.files?.[0]);
              }}
            />
            <p className="text-muted-foreground text-xs">
              Imported entries overwrite matching values. Entries omitted from
              the file stay unchanged. Maximum file size: 2 MB. The complete
              matrix must also fit within the 1 MB save limit.
            </p>
            {reading && (
              <p role="status" className="text-sm">
                Reading CSV…
              </p>
            )}
            {parsed && (
              <p role="status" className="text-sm">
                Ready to import: {parsed.counts.reference} reference claims,{" "}
                {parsed.counts.mapping} platform mappings,{" "}
                {parsed.counts.coverage} coverage claims.
              </p>
            )}
            {message && (
              <p role="alert" className="text-destructive text-sm">
                {message}
              </p>
            )}
          </div>
          <DialogFooter>
            <Button
              variant="ghost"
              disabled={saving}
              onClick={() => changeOpen(false)}
            >
              Cancel
            </Button>
            <Button
              disabled={!parsed || busy || disabled}
              onClick={() => {
                void submit();
              }}
            >
              {saving ? "Importing…" : "Import CSV"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
