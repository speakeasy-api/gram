import { Input } from "@/components/moon/input";
import { ErrorAlert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog } from "@/components/ui/dialog";
import { Type } from "@/components/ui/type";
import type { FetchSkillsFromGitHubResult } from "@gram/client/models/components/fetchskillsfromgithubresult.js";
import type { FetchedGitHubSkill } from "@gram/client/models/components/fetchedgithubskill.js";
import { useCreateSkillMutation } from "@gram/client/react-query/createSkill.js";
import { useFetchSkillsFromGitHubMutation } from "@gram/client/react-query/fetchSkillsFromGitHub.js";
import { useQueryClient } from "@tanstack/react-query";
import { Badge } from "@speakeasy-api/moonshine";
import {
  AlertCircle,
  CheckCircle2,
  FileText,
  GitBranch,
  Loader2,
} from "lucide-react";
import { useEffect, useState } from "react";
import { invalidateSkillQueries } from "./invalidate-skill-queries";
import { SkillValidationErrors } from "./SkillValidationErrors";

function normalizeRepositoryURL(value: string): string {
  const trimmed = value.trim().replace(/\.git$/, "");
  if (/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(trimmed)) {
    return `https://github.com/${trimmed}`;
  }
  if (trimmed.startsWith("github.com/")) return `https://${trimmed}`;
  return trimmed;
}

function importLabel(scanned: boolean, count: number): string {
  if (!scanned) return "Scan repository";
  return `Import ${count} ${count === 1 ? "skill" : "skills"}`;
}

function primaryLabel(
  pending: boolean,
  scanned: boolean,
  count: number,
): string {
  if (pending) return scanned ? "Importing..." : "Scanning...";
  return importLabel(scanned, count);
}

export function GitHubSkillImport({
  onBack,
  onCancel,
  onPendingChange,
}: {
  onBack: () => void;
  onCancel: () => void;
  onPendingChange: (pending: boolean) => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const fetchMutation = useFetchSkillsFromGitHubMutation();
  const createMutation = useCreateSkillMutation();
  const [repositoryURL, setRepositoryURL] = useState("");
  const [result, setResult] = useState<FetchSkillsFromGitHubResult | null>(
    null,
  );
  const [selected, setSelected] = useState<Set<string>>(() => new Set());
  const [importError, setImportError] = useState<string | null>(null);
  const [importing, setImporting] = useState(false);

  const skillCount = result?.skills.length ?? 0;
  const allSelected = skillCount > 0 && selected.size === skillCount;
  const someSelected = selected.size > 0 && !allSelected;
  const isPending = fetchMutation.isPending || importing;
  const error = fetchMutation.error ?? importError;

  useEffect(() => {
    onPendingChange(isPending);
    return () => onPendingChange(false);
  }, [isPending, onPendingChange]);

  const scan = async (): Promise<void> => {
    setImportError(null);
    try {
      const scanned = await fetchMutation.mutateAsync({
        request: {
          fetchSkillsFromGitHubRequestBody: {
            repoUrl: normalizeRepositoryURL(repositoryURL),
          },
        },
      });
      setResult(scanned);
      setSelected(
        new Set(
          scanned.skills
            .filter((skill) => skill.specValid)
            .map((skill) => skill.path),
        ),
      );
    } catch {
      setResult(null);
      setSelected(new Set());
    }
  };

  const importSkills = async (): Promise<void> => {
    if (!result) return;
    setImportError(null);
    setImporting(true);
    const imported = new Set<string>();
    try {
      for (const skill of result.skills) {
        if (!selected.has(skill.path)) continue;
        await createMutation.mutateAsync({
          request: { createSkillRequestBody: { content: skill.content } },
        });
        imported.add(skill.path);
      }
      await invalidateSkillQueries(queryClient);
      onCancel();
    } catch (caught) {
      if (imported.size > 0) {
        setSelected((current) => {
          const next = new Set(current);
          for (const path of imported) next.delete(path);
          return next;
        });
        await invalidateSkillQueries(queryClient);
      }
      setImportError(
        caught instanceof Error ? caught.message : "Unable to import skills.",
      );
    } finally {
      setImporting(false);
    }
  };

  const primaryAction = (): void => {
    if (result) void importSkills();
    else void scan();
  };

  const toggleAll = (): void => {
    setSelected(
      allSelected
        ? new Set()
        : new Set(result?.skills.map((skill) => skill.path)),
    );
  };

  const toggleSkill = (path: string): void => {
    setSelected((current) => {
      const next = new Set(current);
      if (next.has(path)) next.delete(path);
      else next.add(path);
      return next;
    });
  };

  return (
    <>
      <div className="min-h-0 space-y-5 overflow-y-auto pr-1">
        <div className="space-y-2">
          <label htmlFor="github-repository" className="text-sm font-medium">
            Public GitHub repository
          </label>
          <div className="relative">
            <GitBranch
              className="text-muted-foreground pointer-events-none absolute top-2.5 left-3 size-4"
              aria-hidden
            />
            <Input
              id="github-repository"
              value={repositoryURL}
              placeholder="https://github.com/owner/repository"
              autoComplete="off"
              className="pl-9"
              disabled={isPending}
              onChange={(event) => {
                setRepositoryURL(event.currentTarget.value);
                setResult(null);
                setSelected(new Set());
                setImportError(null);
                fetchMutation.reset();
              }}
              onKeyDown={(event) => {
                if (event.key === "Enter" && repositoryURL.trim()) {
                  event.preventDefault();
                  void scan();
                }
              }}
            />
          </div>
          <Type small muted>
            Public repositories only. Gram scans the default branch for every
            SKILL.md file.
          </Type>
        </div>

        {result && (
          <RepositoryResults
            result={result}
            selected={selected}
            allSelected={allSelected}
            someSelected={someSelected}
            onToggleAll={toggleAll}
            onToggleSkill={toggleSkill}
          />
        )}

        {error && (
          <ErrorAlert
            title={
              result ? "Unable to import skills" : "Unable to scan repository"
            }
            error={error}
          />
        )}
      </div>

      <Dialog.Footer className="grid grid-cols-2 sm:flex">
        <Button variant="outline" disabled={isPending} onClick={onBack}>
          Back
        </Button>
        <Button variant="outline" disabled={isPending} onClick={onCancel}>
          Cancel
        </Button>
        <Button
          className="col-span-2"
          disabled={
            isPending ||
            repositoryURL.trim().length === 0 ||
            (result !== null && selected.size === 0)
          }
          onClick={primaryAction}
        >
          {isPending && <Loader2 className="size-4 animate-spin" />}
          {primaryLabel(isPending, result !== null, selected.size)}
        </Button>
      </Dialog.Footer>
    </>
  );
}

function RepositoryResults({
  result,
  selected,
  allSelected,
  someSelected,
  onToggleAll,
  onToggleSkill,
}: {
  result: FetchSkillsFromGitHubResult;
  selected: Set<string>;
  allSelected: boolean;
  someSelected: boolean;
  onToggleAll: () => void;
  onToggleSkill: (path: string) => void;
}): JSX.Element {
  const validCount = result.skills.filter((skill) => skill.specValid).length;
  return (
    <div className="space-y-4">
      <div className="bg-muted/20 flex items-center gap-3 rounded-lg border p-3">
        <div className="bg-background flex size-9 items-center justify-center rounded-md border">
          <GitBranch className="size-4" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <Type className="font-medium">{result.repository.fullName}</Type>
            <Badge variant="neutral">Public</Badge>
          </div>
          <div className="text-muted-foreground flex flex-wrap items-center gap-x-3 text-xs">
            <span className="inline-flex items-center gap-1">
              <GitBranch className="size-3" />
              {result.repository.defaultBranch}
            </span>
            <span className="font-mono">
              {result.repository.commitSha.slice(0, 7)}
            </span>
          </div>
        </div>
      </div>

      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <Type variant="subheading">Skills found</Type>
          <Type small muted>
            {validCount} valid of {result.skills.length + result.issues.length}{" "}
            SKILL.md files
          </Type>
        </div>
        {result.skills.length > 0 && (
          <label className="flex cursor-pointer items-center gap-2 text-sm font-medium">
            <Checkbox
              checked={someSelected ? "indeterminate" : allSelected}
              onCheckedChange={onToggleAll}
            />
            Select all
          </label>
        )}
      </div>

      {result.skills.length === 0 && result.issues.length === 0 && (
        <div className="bg-muted/20 rounded-lg border border-dashed p-8 text-center">
          <Type>No SKILL.md files found</Type>
          <Type small muted>
            The default branch does not contain any skill manifests.
          </Type>
        </div>
      )}

      {result.skills.length > 0 && (
        <div className="divide-border overflow-hidden rounded-lg border">
          {result.skills.map((skill) => (
            <SkillRow
              key={skill.path}
              skill={skill}
              checked={selected.has(skill.path)}
              onToggle={() => onToggleSkill(skill.path)}
            />
          ))}
        </div>
      )}

      {result.issues.length > 0 && (
        <div className="border-destructive/30 bg-destructive/5 space-y-2 rounded-lg border p-3">
          <Type small className="font-medium">
            {result.issues.length} malformed SKILL.md
          </Type>
          {result.issues.map((issue) => (
            <div key={issue.path} className="flex gap-2 text-sm">
              <AlertCircle className="text-destructive mt-0.5 size-4 shrink-0" />
              <span>
                <span className="font-mono text-xs">{issue.path}</span>
                <span className="text-muted-foreground block">
                  {issue.message}
                </span>
              </span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function SkillRow({
  skill,
  checked,
  onToggle,
}: {
  skill: FetchedGitHubSkill;
  checked: boolean;
  onToggle: () => void;
}): JSX.Element {
  return (
    <label className="hover:bg-muted/30 flex cursor-pointer items-start gap-3 border-b p-3 last:border-b-0">
      <Checkbox
        checked={checked}
        onCheckedChange={onToggle}
        aria-label={`Select ${skill.displayName}`}
        className="mt-1"
      />
      <FileText className="text-muted-foreground mt-0.5 size-4" />
      <span className="min-w-0 flex-1">
        <span className="flex flex-wrap items-center gap-2">
          <span className="text-sm font-medium">{skill.displayName}</span>
          {skill.specValid && (
            <CheckCircle2 className="text-success size-3.5" />
          )}
          {!skill.specValid && <Badge variant="warning">Invalid</Badge>}
        </span>
        <span className="text-muted-foreground block text-sm">
          {skill.description || "No description"}
        </span>
        <span className="text-muted-foreground mt-1 block truncate font-mono text-xs">
          {skill.path}
        </span>
        {!skill.specValid && (
          <div className="mt-2">
            <SkillValidationErrors errors={skill.validationErrors} />
          </div>
        )}
      </span>
    </label>
  );
}
