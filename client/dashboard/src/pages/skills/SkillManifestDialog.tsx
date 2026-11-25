import { ErrorAlert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Textarea } from "@/components/moon/textarea";
import { Text } from "@/components/ui/Text";
import { useQueryState } from "nuqs";
import type { ApproveSkillSuggestionResultOutcome } from "@gram/client/models/components/approveskillsuggestionresult.js";
import type { RecordSkillResult } from "@gram/client/models/components/recordskillresult.js";
import type { SkillFile } from "@gram/client/models/components/skillfile.js";
import type { SkillFileInput } from "@gram/client/models/components/skillfileinput.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { buildSkillVersionFileQuery } from "@gram/client/react-query/skillVersionFile.core.js";
import { useAddSkillVersionMutation } from "@gram/client/react-query/addSkillVersion.js";
import { useApproveSkillSuggestionMutation } from "@gram/client/react-query/approveSkillSuggestion.js";
import { useCreateSkillMutation } from "@gram/client/react-query/createSkill.js";
import { useQueryClient } from "@tanstack/react-query";
import { X } from "lucide-react";
import { useId, useRef, useState } from "react";
import { toast } from "sonner";
import {
  decodeManifestFile,
  manifestByteLength,
  MAX_SKILL_MANIFEST_BYTES,
  SKILL_MANIFEST_TEMPLATE,
  validateManifestContent,
} from "./skill-manifest";
import { invalidateSkillQueries } from "./invalidate-skill-queries";
import {
  type DraftSkillFile,
  formatFileSize,
  type SkillUploadEntry,
  isSkillScript,
  loadDraftFileContent,
  parseSkillUpload,
  readSkillFolder,
  readSkillZipFile,
  rootNameWarning,
  SkillUploadError,
  validateSkillFiles,
} from "./skill-files";
import { SkillValidationErrors } from "./SkillValidationErrors";

export type SkillManifestDialogMode = "create" | "edit" | "approve-suggestion";

const MODE_COPY: Record<
  SkillManifestDialogMode,
  { title: string; description: string; submit: string }
> = {
  create: {
    title: "Add skill",
    description:
      "Paste a SKILL.md manifest, or upload a skill folder or .zip to include its scripts, references, and assets.",
    submit: "Add skill",
  },
  edit: {
    title: "Edit skill",
    description:
      "Edit SKILL.md or its supporting files. Saving records your changes as a new immutable version.",
    submit: "Save new version",
  },
  "approve-suggestion": {
    title: "Edit and approve suggestion",
    description:
      "Review the complete proposed SKILL.md. Approval records a new version unless the skill changed, in which case the suggestion is superseded.",
    submit: "Approve edited suggestion",
  },
};

export function SkillManifestDialog({
  mode,
  open,
  onOpenChange,
  skillId,
  derivedFromVersionId,
  suggestionId,
  onSuggestionApproved,
  initialContent = "",
  existingFiles = [],
}: {
  mode: SkillManifestDialogMode;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  skillId?: string;
  derivedFromVersionId?: string;
  suggestionId?: string;
  onSuggestionApproved?: (outcome: ApproveSkillSuggestionResultOutcome) => void;
  initialContent?: string;
  /** Supporting files of the version being edited, kept unless the user changes them. */
  existingFiles?: SkillFile[];
}): JSX.Element {
  const copy = MODE_COPY[mode];
  const [, setSelectedSkillId] = useQueryState("skill");
  const queryClient = useQueryClient();
  const client = useGramContext();
  // Suggestions only change SKILL.md, so approving one keeps the base files.
  const managesFiles = mode !== "approve-suggestion";
  const initialDraftFiles = (): DraftSkillFile[] =>
    existingFiles.map((file) => ({
      kind: "existing",
      path: file.path,
      sizeBytes: file.sizeBytes,
      contentType: file.contentType,
    }));
  const fieldId = useId();
  const helpId = `${fieldId}-help`;
  const errorId = `${fieldId}-error`;
  const [content, setContent] = useState(initialContent);
  const [fieldError, setFieldError] = useState<string | null>(null);
  const [mutationError, setMutationError] = useState<string | null>(null);
  const [reconcilingApproval, setReconcilingApproval] = useState(false);
  const [approvalUncertain, setApprovalUncertain] = useState(false);
  const [savedResult, setSavedResult] = useState<RecordSkillResult | null>(
    null,
  );
  const [continuing, setContinuing] = useState(false);
  const [persistedSkillId, setPersistedSkillId] = useState<string | null>(null);
  const [parentVersionId, setParentVersionId] = useState(derivedFromVersionId);
  const [noOpContent, setNoOpContent] = useState<string | null>(null);
  const [readingFile, setReadingFile] = useState(false);
  const fileReadSeq = useRef(0);
  const [draftFiles, setDraftFiles] =
    useState<DraftSkillFile[]>(initialDraftFiles);
  // Files are sent only when the user changed the set; otherwise the new
  // version keeps the base version's files on the server.
  const [filesRevision, setFilesRevision] = useState(0);
  const [uploadRootDir, setUploadRootDir] = useState<string | null>(null);
  const [filesErrors, setFilesErrors] = useState<string[]>([]);
  // A folder or zip that could not be read. Saving stays blocked until the
  // user uploads again or dismisses it, so the previous draft is never saved
  // by mistake in its place.
  const [uploadError, setUploadError] = useState<string | null>(null);
  const [noOpFilesRevision, setNoOpFilesRevision] = useState<number | null>(
    null,
  );
  const createMutation = useCreateSkillMutation();
  const addVersionMutation = useAddSkillVersionMutation();
  const approveSuggestionMutation = useApproveSkillSuggestionMutation();
  const isPending =
    createMutation.isPending ||
    addVersionMutation.isPending ||
    approveSuggestionMutation.isPending;
  const isBusy = isPending || reconcilingApproval;
  const savedInvalid = savedResult?.version.specValid === false;
  const noChanges = savedResult?.createdVersion === false;
  const unchangedNoOp =
    noOpContent === content && noOpFilesRevision === filesRevision;
  const filesChanged = filesRevision > 0;
  const nameWarning = rootNameWarning(content, uploadRootDir);
  const submitLabel = persistedSkillId ? "Add version" : copy.submit;

  const reset = (): void => {
    setContent(initialContent);
    setFieldError(null);
    setMutationError(null);
    setReconcilingApproval(false);
    setApprovalUncertain(false);
    setSavedResult(null);
    setContinuing(false);
    setPersistedSkillId(null);
    setParentVersionId(derivedFromVersionId);
    setNoOpContent(null);
    setNoOpFilesRevision(null);
    setDraftFiles(initialDraftFiles());
    setFilesRevision(0);
    setUploadRootDir(null);
    setFilesErrors([]);
    setUploadError(null);
    fileReadSeq.current += 1;
    setReadingFile(false);
    createMutation.reset();
    addVersionMutation.reset();
    approveSuggestionMutation.reset();
  };

  const handleOpenChange = (nextOpen: boolean): void => {
    if (!nextOpen && isBusy) return;
    if (!nextOpen) reset();
    onOpenChange(nextOpen);
  };

  const resolveFileInputs = async (): Promise<SkillFileInput[] | undefined> => {
    if (!managesFiles || !filesChanged) return undefined;
    // Uploaded files left unread (the upload was over the limits before
    // files were removed) are read now, one at a time, since a zip entry is
    // re-inflated from the whole archive.
    const uploaded = new Map<string, Uint8Array>();
    for (const file of draftFiles) {
      if (file.kind === "new") {
        uploaded.set(file.path, await loadDraftFileContent(file));
      }
    }
    return Promise.all(
      draftFiles.map(async (file): Promise<SkillFileInput> => {
        if (file.kind === "new") {
          const content = uploaded.get(file.path);
          if (!content) throw new Error(`Unable to read ${file.path}.`);
          return { path: file.path, content };
        }
        // A kept file's bytes live on the server; fetch them so the request
        // can carry the complete new set.
        if (!skillId || !derivedFromVersionId) {
          throw new Error(`Unable to keep ${file.path}.`);
        }
        const stored = await queryClient.fetchQuery({
          ...buildSkillVersionFileQuery(client, {
            skillId,
            versionId: derivedFromVersionId,
            path: file.path,
          }),
          staleTime: Infinity,
        });
        return {
          path: file.path,
          content: stored.content,
          contentType: file.contentType,
        };
      }),
    );
  };

  const submit = async (): Promise<void> => {
    if (uploadError) return;
    const validationError = validateManifestContent(content);
    setFieldError(validationError);
    setMutationError(null);
    const fileErrors = managesFiles ? validateSkillFiles(draftFiles) : [];
    setFilesErrors(fileErrors);
    if (validationError || fileErrors.length > 0) return;

    try {
      if (mode === "approve-suggestion") {
        if (!suggestionId) {
          setMutationError("A suggestion ID is required to approve this edit.");
          return;
        }
        let approval:
          | Awaited<ReturnType<typeof approveSuggestionMutation.mutateAsync>>
          | undefined;
        setReconcilingApproval(true);
        try {
          approval = await approveSuggestionMutation.mutateAsync({
            request: {
              approveSkillSuggestionRequestBody: { id: suggestionId, content },
            },
          });
        } catch (error) {
          const message =
            error instanceof Error
              ? error.message
              : "Unable to approve suggestion.";
          setMutationError(
            `${message} Approval status may be unknown. Review the refreshed state before retrying.`,
          );
          setApprovalUncertain(true);
        } finally {
          await invalidateSkillQueries(queryClient).catch(() => undefined);
          setReconcilingApproval(false);
        }
        if (!approval) return;
        onSuggestionApproved?.(approval.outcome);
        handleOpenChange(false);
        if (approval.outcome === "applied") {
          toast.success("Suggested edit approved");
        } else {
          toast.info("Suggestion was superseded because the skill changed");
        }
        return;
      }

      let result: RecordSkillResult;
      const versionSkillId = persistedSkillId ?? skillId;
      const files = await resolveFileInputs();
      if (mode === "create" && !versionSkillId) {
        result = await createMutation.mutateAsync({
          request: { createSkillRequestBody: { content, files } },
        });
      } else {
        if (!versionSkillId) {
          setMutationError("A skill ID is required to add a version.");
          return;
        }
        result = await addVersionMutation.mutateAsync({
          request: {
            addSkillVersionRequestBody: {
              id: versionSkillId,
              content,
              derivedFromVersionId: parentVersionId,
              files,
            },
          },
        });
      }

      if (!result.createdVersion) {
        setPersistedSkillId(result.skill.id);
        setParentVersionId(result.version.id);
        setNoOpContent(content);
        setNoOpFilesRevision(filesRevision);
        setSavedResult(result);
        return;
      }

      await invalidateSkillQueries(queryClient);
      if (!result.version.specValid) {
        setPersistedSkillId(result.skill.id);
        setParentVersionId(result.version.id);
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

  const handleSkillUpload = async (
    read: () => Promise<SkillUploadEntry[]> | SkillUploadEntry[],
  ): Promise<void> => {
    const seq = ++fileReadSeq.current;
    setReadingFile(true);
    try {
      const parsed = parseSkillUpload(await read());
      if (seq !== fileReadSeq.current) return;
      setUploadError(null);
      setContent(parsed.manifest);
      setDraftFiles(parsed.files);
      setFilesRevision((revision) => revision + 1);
      setUploadRootDir(parsed.rootDir);
      setSavedResult(null);
      setContinuing(false);
      setFieldError(validateManifestContent(parsed.manifest));
      setFilesErrors(validateSkillFiles(parsed.files));
    } catch (error) {
      if (seq !== fileReadSeq.current) return;
      setUploadError(
        error instanceof SkillUploadError
          ? error.message
          : "Unable to read the selected files.",
      );
    } finally {
      if (seq === fileReadSeq.current) setReadingFile(false);
    }
  };

  const removeFile = (path: string): void => {
    const next = draftFiles.filter((file) => file.path !== path);
    setDraftFiles(next);
    setFilesRevision((revision) => revision + 1);
    setSavedResult(null);
    setFilesErrors((errors) =>
      errors.length > 0 ? validateSkillFiles(next) : errors,
    );
  };

  const insertTemplate = (): void => {
    setContent(SKILL_MANIFEST_TEMPLATE);
    setFieldError(null);
    if (!approvalUncertain) setMutationError(null);
    setSavedResult(null);
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

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <Dialog.Content className="max-h-[calc(100vh-2rem)] grid-rows-[auto_minmax(0,1fr)_auto] sm:max-w-3xl">
        <Dialog.Header>
          <Dialog.Title>{copy.title}</Dialog.Title>
          <Dialog.Description>{copy.description}</Dialog.Description>
        </Dialog.Header>

        <div className="min-h-0 space-y-4 overflow-y-auto pr-1">
          <SavedManifestResult
            result={savedResult}
            noChanges={noChanges}
            onContinue={continueEditing}
            onView={viewSkill}
          />

          {continuing && (
            <Text small muted role="status">
              Future saves add versions to this saved skill. Change the manifest
              before saving again.
            </Text>
          )}

          <div className="space-y-2">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <label htmlFor={fieldId} className="text-sm font-medium">
                SKILL.md content
              </label>
              <div className="flex flex-wrap items-center gap-3">
                {mode === "create" && (
                  <Button
                    type="button"
                    variant="tertiary"
                    size="xs"
                    className="h-auto p-0"
                    disabled={isBusy || savedInvalid}
                    onClick={insertTemplate}
                  >
                    Insert template
                  </Button>
                )}
                <label className="text-primary cursor-pointer text-sm font-medium hover:underline">
                  Upload SKILL.md
                  <input
                    type="file"
                    accept=".md,text/markdown,text/plain"
                    className="sr-only"
                    disabled={isBusy || savedInvalid}
                    onChange={(event) => {
                      void handleFile(event.currentTarget.files?.[0]);
                      event.currentTarget.value = "";
                    }}
                  />
                </label>
                {managesFiles && (
                  <>
                    <label className="text-primary cursor-pointer text-sm font-medium hover:underline">
                      Upload folder
                      <input
                        type="file"
                        multiple
                        className="sr-only"
                        aria-label="Upload skill folder"
                        disabled={isBusy || savedInvalid}
                        {...{ webkitdirectory: "" }}
                        onChange={(event) => {
                          const picked = Array.from(
                            event.currentTarget.files ?? [],
                          );
                          if (picked.length > 0) {
                            void handleSkillUpload(() =>
                              readSkillFolder(picked),
                            );
                          }
                          event.currentTarget.value = "";
                        }}
                      />
                    </label>
                    <label className="text-primary cursor-pointer text-sm font-medium hover:underline">
                      Upload .zip
                      <input
                        type="file"
                        accept=".zip,application/zip"
                        className="sr-only"
                        aria-label="Upload skill .zip"
                        disabled={isBusy || savedInvalid}
                        onChange={(event) => {
                          const zip = event.currentTarget.files?.[0];
                          if (zip) {
                            void handleSkillUpload(() => readSkillZipFile(zip));
                          }
                          event.currentTarget.value = "";
                        }}
                      />
                    </label>
                  </>
                )}
              </div>
            </div>
            <Textarea
              id={fieldId}
              value={content}
              rows={18}
              disabled={isBusy || savedInvalid}
              className="min-h-64 resize-y font-mono text-sm"
              aria-invalid={fieldError !== null}
              aria-describedby={`${helpId}${fieldError ? ` ${errorId}` : ""}`}
              onChange={(event) => {
                setContent(event.currentTarget.value);
                setFieldError(null);
                if (!approvalUncertain) setMutationError(null);
                setSavedResult(null);
              }}
            />
            <div className="flex flex-wrap justify-between gap-2">
              <Text id={helpId} small muted>
                UTF-8, up to 65,536 bytes.
              </Text>
              <Text small muted className="font-mono">
                {manifestByteLength(content).toLocaleString()} bytes
              </Text>
            </div>
            <div id={errorId} aria-live="polite">
              {fieldError && (
                <Text small className="text-destructive">
                  {fieldError}
                </Text>
              )}
            </div>
          </div>

          {uploadError && (
            <div
              role="alert"
              className="border-destructive/40 flex items-start justify-between gap-3 border px-3 py-2"
            >
              <Text small className="text-destructive">
                {uploadError} Upload again, or dismiss this to keep the current
                draft.
              </Text>
              <Button
                type="button"
                variant="tertiary"
                size="xs"
                className="h-auto shrink-0 p-0"
                onClick={() => setUploadError(null)}
              >
                Dismiss
              </Button>
            </div>
          )}

          {managesFiles && (
            <SupportingFilesField
              files={draftFiles}
              errors={filesErrors}
              nameWarning={nameWarning}
              replacesOnUpload={mode === "edit"}
              disabled={isBusy || savedInvalid}
              onRemove={removeFile}
            />
          )}

          {mutationError && (
            <ErrorAlert
              title={
                mode === "approve-suggestion"
                  ? "Unable to approve suggestion"
                  : "Unable to save skill"
              }
              error={mutationError}
            />
          )}
        </div>

        <Dialog.Footer>
          <Button
            variant="secondary"
            disabled={isBusy}
            onClick={() => handleOpenChange(false)}
          >
            Cancel
          </Button>
          <Button
            onClick={() => void submit()}
            disabled={
              isBusy ||
              readingFile ||
              uploadError !== null ||
              savedInvalid ||
              unchangedNoOp ||
              approvalUncertain
            }
          >
            {isBusy ? "Saving..." : submitLabel}
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}

function SupportingFilesField({
  files,
  errors,
  nameWarning,
  replacesOnUpload,
  disabled,
  onRemove,
}: {
  files: DraftSkillFile[];
  errors: string[];
  nameWarning: string | null;
  replacesOnUpload: boolean;
  disabled: boolean;
  onRemove: (path: string) => void;
}): JSX.Element {
  const total = files.reduce((sum, file) => sum + file.sizeBytes, 0);
  return (
    <section aria-label="Supporting files" className="space-y-2">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <span className="text-sm font-medium">Supporting files</span>
        {files.length > 0 && (
          <Text small muted className="font-mono">
            {files.length} {files.length === 1 ? "file" : "files"} ·{" "}
            {formatFileSize(total)}
          </Text>
        )}
      </div>
      {files.length === 0 ? (
        <Text small muted>
          None. Upload a skill folder or .zip to add scripts, references, or
          assets.
        </Text>
      ) : (
        <ul className="divide-border max-h-56 divide-y overflow-y-auto border">
          {files.map((file) => (
            <li
              key={file.path}
              className="flex items-center gap-3 px-3 py-1.5 text-sm"
            >
              <span
                className="min-w-0 flex-1 truncate font-mono"
                title={file.path}
              >
                {file.path}
              </span>
              {isSkillScript(file.path) && (
                <Text small muted>
                  Script
                </Text>
              )}
              <Text small muted className="shrink-0 font-mono">
                {formatFileSize(file.sizeBytes)}
              </Text>
              <Button
                type="button"
                variant="tertiary"
                size="xs"
                className="h-6 w-6 shrink-0 p-0"
                disabled={disabled}
                aria-label={`Remove ${file.path}`}
                onClick={() => onRemove(file.path)}
              >
                <X className="h-3.5 w-3.5" />
              </Button>
            </li>
          ))}
        </ul>
      )}
      <Text small muted>
        {replacesOnUpload
          ? "Uploading a folder or .zip replaces SKILL.md and every supporting file. "
          : ""}
        Up to 100 files, 5 MB each and 10 MB in total.
      </Text>
      {nameWarning && (
        <Text small className="text-warning" role="status">
          {nameWarning}
        </Text>
      )}
      {errors.length > 0 && (
        <ul aria-live="polite" className="space-y-1">
          {errors.map((error) => (
            <li key={error}>
              <Text small className="text-destructive">
                {error}
              </Text>
            </li>
          ))}
        </ul>
      )}
    </section>
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
      className="border-border bg-muted/30 space-y-3 border p-4"
      role="status"
    >
      <Text variant="subheading">{title}</Text>
      {!noChanges && (
        <SkillValidationErrors errors={result.version.validationErrors} />
      )}
      <Text small muted>
        Continue editing keeps this saved skill selected. Future saves create
        immutable versions of it.
      </Text>
      <div className="flex flex-wrap gap-2">
        <Button size="sm" onClick={onView}>
          View skill
        </Button>
        <Button size="sm" variant="secondary" onClick={onContinue}>
          Continue editing
        </Button>
      </div>
    </div>
  );
}
