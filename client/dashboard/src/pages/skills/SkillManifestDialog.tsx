import { ErrorAlert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { Textarea } from "@/components/moon/textarea";
import { Type } from "@/components/ui/type";
import { useQueryState } from "nuqs";
import type { RecordSkillResult } from "@gram/client/models/components/recordskillresult.js";
import { useAddSkillVersionMutation } from "@gram/client/react-query/addSkillVersion.js";
import { useCreateSkillMutation } from "@gram/client/react-query/createSkill.js";
import { useQueryClient } from "@tanstack/react-query";
import { useId, useRef, useState, type ReactNode } from "react";
import { FileText, GitBranch } from "lucide-react";
import {
  decodeManifestFile,
  manifestByteLength,
  MAX_SKILL_MANIFEST_BYTES,
  validateManifestContent,
} from "./skill-manifest";
import { invalidateSkillQueries } from "./invalidate-skill-queries";
import { SkillValidationErrors } from "./SkillValidationErrors";
import { GitHubSkillImport } from "./GitHubSkillImport";

export type SkillManifestDialogMode = "create" | "edit";
type CreateSource = "manual" | "github" | null;

const MODE_COPY: Record<
  SkillManifestDialogMode,
  { title: string; description: string; submit: string }
> = {
  create: {
    title: "Add skill",
    description: "Paste a SKILL.md manifest or upload a Markdown file.",
    submit: "Add skill",
  },
  edit: {
    title: "Edit skill",
    description: "Saving records your changes as a new immutable version.",
    submit: "Save new version",
  },
};

export function SkillManifestDialog({
  mode,
  open,
  onOpenChange,
  skillId,
  initialContent = "",
}: {
  mode: SkillManifestDialogMode;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  skillId?: string;
  initialContent?: string;
}): JSX.Element {
  const copy = MODE_COPY[mode];
  const [, setSelectedSkillId] = useQueryState("skill");
  const queryClient = useQueryClient();
  const fieldId = useId();
  const helpId = `${fieldId}-help`;
  const errorId = `${fieldId}-error`;
  const [content, setContent] = useState(initialContent);
  const [fieldError, setFieldError] = useState<string | null>(null);
  const [mutationError, setMutationError] = useState<string | null>(null);
  const [savedResult, setSavedResult] = useState<RecordSkillResult | null>(
    null,
  );
  const [continuing, setContinuing] = useState(false);
  const [persistedSkillId, setPersistedSkillId] = useState<string | null>(null);
  const [noOpContent, setNoOpContent] = useState<string | null>(null);
  const [readingFile, setReadingFile] = useState(false);
  const [createSource, setCreateSource] = useState<CreateSource>(
    mode === "create" ? null : "manual",
  );
  const fileReadSeq = useRef(0);
  const createMutation = useCreateSkillMutation();
  const addVersionMutation = useAddSkillVersionMutation();
  const [importPending, setImportPending] = useState(false);
  const isPending =
    createMutation.isPending || addVersionMutation.isPending || importPending;
  const savedInvalid = savedResult?.version.specValid === false;
  const noChanges = savedResult?.createdVersion === false;
  const unchangedNoOp = noOpContent === content;
  const submitLabel = persistedSkillId ? "Add version" : copy.submit;

  const reset = (): void => {
    setContent(initialContent);
    setFieldError(null);
    setMutationError(null);
    setSavedResult(null);
    setContinuing(false);
    setPersistedSkillId(null);
    setNoOpContent(null);
    fileReadSeq.current += 1;
    setReadingFile(false);
    setImportPending(false);
    setCreateSource(mode === "create" ? null : "manual");
    createMutation.reset();
    addVersionMutation.reset();
  };

  const handleOpenChange = (nextOpen: boolean): void => {
    if (!nextOpen && isPending) return;
    if (!nextOpen) reset();
    onOpenChange(nextOpen);
  };

  const submit = async (): Promise<void> => {
    const validationError = validateManifestContent(content);
    setFieldError(validationError);
    setMutationError(null);
    if (validationError) return;

    try {
      let result: RecordSkillResult;
      const versionSkillId = persistedSkillId ?? skillId;
      if (mode === "create" && !versionSkillId) {
        result = await createMutation.mutateAsync({
          request: { createSkillRequestBody: { content } },
        });
      } else {
        if (!versionSkillId) {
          setMutationError("A skill ID is required to add a version.");
          return;
        }
        result = await addVersionMutation.mutateAsync({
          request: {
            addSkillVersionRequestBody: { id: versionSkillId, content },
          },
        });
      }

      if (!result.createdVersion) {
        setPersistedSkillId(result.skill.id);
        setNoOpContent(content);
        setSavedResult(result);
        return;
      }

      await invalidateSkillQueries(queryClient);
      if (!result.version.specValid) {
        setPersistedSkillId(result.skill.id);
        setNoOpContent(null);
        setSavedResult(result);
        return;
      }

      handleOpenChange(false);
      void setSelectedSkillId(result.skill.id);
    } catch (error) {
      setMutationError(
        error instanceof Error ? error.message : "Unable to save SKILL.md.",
      );
    }
  };

  const handleFile = async (file: File | undefined): Promise<void> => {
    if (!file) return;
    if (file.size > MAX_SKILL_MANIFEST_BYTES) {
      setFieldError(
        `SKILL.md must be 65,536 bytes or fewer (currently ${file.size.toLocaleString()} bytes).`,
      );
      return;
    }
    const seq = ++fileReadSeq.current;
    setReadingFile(true);
    try {
      const nextContent = decodeManifestFile(await file.arrayBuffer());
      if (seq !== fileReadSeq.current) return;
      setContent(nextContent);
      setSavedResult(null);
      setContinuing(false);
      setFieldError(validateManifestContent(nextContent));
    } catch {
      if (seq !== fileReadSeq.current) return;
      setFieldError("The selected file is not valid UTF-8.");
    } finally {
      if (seq === fileReadSeq.current) setReadingFile(false);
    }
  };

  const continueEditing = (): void => {
    setSavedResult(null);
    setContinuing(true);
  };

  const viewSkill = (): void => {
    if (!savedResult) return;
    handleOpenChange(false);
    void setSelectedSkillId(savedResult.skill.id);
  };

  let title = copy.title;
  let description = copy.description;
  if (mode === "create" && createSource === null) {
    description = "Choose how you want to add skills to this project.";
  }
  if (mode === "create" && createSource === "github") {
    title = "Import from GitHub";
    description =
      "Scan a public repository for valid SKILL.md files and choose what to import.";
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <Dialog.Content className="max-h-[calc(100vh-2rem)] grid-rows-[auto_minmax(0,1fr)_auto] sm:max-w-3xl">
        <Dialog.Header>
          <Dialog.Title>{title}</Dialog.Title>
          <Dialog.Description>{description}</Dialog.Description>
        </Dialog.Header>

        {mode === "create" && createSource === null && (
          <>
            <div className="grid gap-3 sm:grid-cols-2">
              <SourceOption
                title="Manual upload"
                description="Paste a SKILL.md manifest or upload a Markdown file."
                icon={<FileText className="size-5" />}
                onClick={() => setCreateSource("manual")}
              />
              <SourceOption
                title="GitHub repository"
                description="Scan a public repository and select skills to import."
                icon={<GitBranch className="size-5" />}
                onClick={() => setCreateSource("github")}
              />
            </div>
            <Dialog.Footer>
              <Button variant="outline" onClick={() => handleOpenChange(false)}>
                Cancel
              </Button>
            </Dialog.Footer>
          </>
        )}
        {createSource === "github" && (
          <GitHubSkillImport
            onBack={() => setCreateSource(null)}
            onCancel={() => handleOpenChange(false)}
            onPendingChange={setImportPending}
          />
        )}
        {createSource !== null && createSource !== "github" && (
          <>
            <div className="min-h-0 space-y-4 overflow-y-auto pr-1">
              <SavedManifestResult
                result={savedResult}
                noChanges={noChanges}
                onContinue={continueEditing}
                onView={viewSkill}
              />

              {continuing && (
                <Type small muted role="status">
                  Future saves add versions to this saved skill. Change the
                  manifest before saving again.
                </Type>
              )}

              <div className="space-y-2">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <label htmlFor={fieldId} className="text-sm font-medium">
                    SKILL.md content
                  </label>
                  <label className="text-primary cursor-pointer text-sm font-medium hover:underline">
                    Upload .md file
                    <input
                      type="file"
                      accept=".md,text/markdown,text/plain"
                      className="sr-only"
                      disabled={isPending || savedInvalid}
                      onChange={(event) => {
                        void handleFile(event.currentTarget.files?.[0]);
                        event.currentTarget.value = "";
                      }}
                    />
                  </label>
                </div>
                <Textarea
                  id={fieldId}
                  value={content}
                  rows={18}
                  disabled={isPending || savedInvalid}
                  className="min-h-64 resize-y font-mono text-sm"
                  aria-invalid={fieldError !== null}
                  aria-describedby={`${helpId}${fieldError ? ` ${errorId}` : ""}`}
                  onChange={(event) => {
                    setContent(event.currentTarget.value);
                    setFieldError(null);
                    setMutationError(null);
                    setSavedResult(null);
                  }}
                />
                <div className="flex flex-wrap justify-between gap-2">
                  <Type id={helpId} small muted>
                    UTF-8, up to 65,536 bytes.
                  </Type>
                  <Type small muted className="font-mono">
                    {manifestByteLength(content).toLocaleString()} bytes
                  </Type>
                </div>
                <div id={errorId} aria-live="polite">
                  {fieldError && (
                    <Type small className="text-destructive">
                      {fieldError}
                    </Type>
                  )}
                </div>
              </div>

              {mutationError && (
                <ErrorAlert
                  title="Unable to save skill"
                  error={mutationError}
                />
              )}
            </div>

            <Dialog.Footer>
              {mode === "create" && (
                <Button
                  variant="outline"
                  disabled={isPending}
                  onClick={() => setCreateSource(null)}
                >
                  Back
                </Button>
              )}
              <Button
                variant="outline"
                disabled={isPending}
                onClick={() => handleOpenChange(false)}
              >
                Cancel
              </Button>
              <Button
                onClick={() => void submit()}
                disabled={
                  isPending || readingFile || savedInvalid || unchangedNoOp
                }
              >
                {isPending ? "Saving..." : submitLabel}
              </Button>
            </Dialog.Footer>
          </>
        )}
      </Dialog.Content>
    </Dialog>
  );
}

function SourceOption({
  title,
  description,
  icon,
  onClick,
}: {
  title: string;
  description: string;
  icon: ReactNode;
  onClick: () => void;
}): JSX.Element {
  return (
    <button
      type="button"
      className="hover:bg-muted/30 focus-visible:border-ring focus-visible:ring-ring/50 flex min-h-36 flex-col items-start gap-3 rounded-lg border p-5 text-left outline-none focus-visible:ring-[3px]"
      onClick={onClick}
    >
      <span className="bg-muted flex size-10 items-center justify-center rounded-md">
        {icon}
      </span>
      <span>
        <span className="block font-medium">{title}</span>
        <span className="text-muted-foreground mt-1 block text-sm">
          {description}
        </span>
      </span>
    </button>
  );
}

function SavedManifestResult({
  result,
  noChanges,
  onContinue,
  onView,
}: {
  result: RecordSkillResult | null;
  noChanges: boolean;
  onContinue: () => void;
  onView: () => void;
}): JSX.Element | null {
  if (!result) return null;

  let title = "Saved with validation issues.";
  if (noChanges) title = "No changes detected.";
  return (
    <div
      className="border-border bg-muted/30 space-y-3 rounded-lg border p-4"
      role="status"
    >
      <Type variant="subheading">{title}</Type>
      {!noChanges && (
        <SkillValidationErrors errors={result.version.validationErrors} />
      )}
      <Type small muted>
        Continue editing keeps this saved skill selected. Future saves create
        immutable versions of it.
      </Type>
      <div className="flex flex-wrap gap-2">
        <Button size="sm" onClick={onView}>
          View skill
        </Button>
        <Button size="sm" variant="outline" onClick={onContinue}>
          Continue editing
        </Button>
      </div>
    </div>
  );
}
