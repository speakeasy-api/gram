import { ErrorAlert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Textarea } from "@/components/moon/textarea";
import { Text } from "@/components/ui/Text";
import { useQueryState } from "nuqs";
import type { ApproveSkillSuggestionResultOutcome } from "@gram/client/models/components/approveskillsuggestionresult.js";
import type { RecordSkillResult } from "@gram/client/models/components/recordskillresult.js";
import { useAddSkillVersionMutation } from "@gram/client/react-query/addSkillVersion.js";
import { useApproveSkillSuggestionMutation } from "@gram/client/react-query/approveSkillSuggestion.js";
import { useCreateSkillMutation } from "@gram/client/react-query/createSkill.js";
import { useQueryClient } from "@tanstack/react-query";
import { useId, useRef, useState } from "react";
import { toast } from "sonner";
import {
  decodeManifestFile,
  manifestByteLength,
  MAX_SKILL_MANIFEST_BYTES,
  validateManifestContent,
} from "./skill-manifest";
import { invalidateSkillQueries } from "./invalidate-skill-queries";
import { SkillValidationErrors } from "./SkillValidationErrors";

export type SkillManifestDialogMode = "create" | "edit" | "approve-suggestion";

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
    title: "Edit SKILL.md",
    description: "Saving records your changes as a new immutable version.",
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
}: {
  mode: SkillManifestDialogMode;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  skillId?: string;
  derivedFromVersionId?: string;
  suggestionId?: string;
  onSuggestionApproved?: (outcome: ApproveSkillSuggestionResultOutcome) => void;
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
  const unchangedNoOp = noOpContent === content;
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

  const submit = async (): Promise<void> => {
    const validationError = validateManifestContent(content);
    setFieldError(validationError);
    setMutationError(null);
    if (validationError) return;

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
            addSkillVersionRequestBody: {
              id: versionSkillId,
              content,
              derivedFromVersionId: parentVersionId,
            },
          },
        });
      }

      if (!result.createdVersion) {
        setPersistedSkillId(result.skill.id);
        setParentVersionId(result.version.id);
        setNoOpContent(content);
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
              <label className="text-primary cursor-pointer text-sm font-medium hover:underline">
                Upload .md file
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
