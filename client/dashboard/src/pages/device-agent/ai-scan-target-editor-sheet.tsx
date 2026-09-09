import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/Field";
import { Input } from "@/components/ui/Input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { TagInput } from "@/components/ui/TagInput";
import { Text } from "@/components/ui/Text";
import type { UpsertRequestBody2 } from "@gram/client/models/components/upsertrequestbody2.js";
import { useState } from "react";
import {
  draftToUpsertBody,
  slugFromName,
  TARGET_CATEGORIES,
  validateDraft,
  type Draft,
  type DraftErrors,
  type TargetCategory,
} from "./ai-scan-target-draft";

export type EditorMode = "create" | "edit";

// idDescription explains the id agents will report for the name typed so far.
function idDescription(id: string, mode: EditorMode): React.ReactNode {
  if (id === "") {
    return "Agents report the target by an id derived from this name.";
  }
  const suffix =
    mode === "create"
      ? "The id is set once and never reused for a different tool."
      : "The id is fixed.";
  return (
    <>
      Agents report this target as <span className="font-mono">{id}</span>.{" "}
      {suffix}
    </>
  );
}

type EditorSheetProps = {
  open: boolean;
  mode: EditorMode;
  initialDraft: Draft;
  pending: boolean;
  serverError: string | null;
  onOpenChange: (open: boolean) => void;
  onSubmit: (body: UpsertRequestBody2) => void;
};

function SignatureField({
  id,
  label,
  description,
  placeholder,
  value,
  error,
  onChange,
}: {
  id: string;
  label: string;
  description: string;
  placeholder: string;
  value: string[];
  error?: string;
  onChange: (value: string[]) => void;
}): JSX.Element {
  return (
    <Field>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <TagInput
        id={id}
        value={value}
        onChange={onChange}
        placeholder={placeholder}
        error={error !== undefined}
      />
      <FieldDescription>{description}</FieldDescription>
      <FieldError>{error}</FieldError>
    </Field>
  );
}

export function AiScanTargetEditorSheet({
  open,
  mode,
  initialDraft,
  pending,
  serverError,
  onOpenChange,
  onSubmit,
}: EditorSheetProps): JSX.Element {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="flex w-[560px] flex-col gap-0 sm:max-w-[560px]">
        {open ? (
          <EditorForm
            key={`${mode}:${initialDraft.id}`}
            mode={mode}
            initialDraft={initialDraft}
            pending={pending}
            serverError={serverError}
            onCancel={() => onOpenChange(false)}
            onSubmit={onSubmit}
          />
        ) : null}
      </SheetContent>
    </Sheet>
  );
}

// EditorForm is keyed on the target being edited so switching rows resets it.
function EditorForm({
  mode,
  initialDraft,
  pending,
  serverError,
  onCancel,
  onSubmit,
}: {
  mode: EditorMode;
  initialDraft: Draft;
  pending: boolean;
  serverError: string | null;
  onCancel: () => void;
  onSubmit: (body: UpsertRequestBody2) => void;
}): JSX.Element {
  const [draft, setDraft] = useState<Draft>(initialDraft);
  const [errors, setErrors] = useState<DraftErrors>({});
  const update = <K extends keyof Draft>(key: K, value: Draft[K]): void => {
    setDraft((current) => ({ ...current, [key]: value }));
  };

  const id = mode === "create" ? slugFromName(draft.displayName) : draft.id;

  const handleSubmit = (event: React.FormEvent<HTMLFormElement>): void => {
    event.preventDefault();
    const candidate = { ...draft, id };
    const found = validateDraft(candidate);
    setErrors(found);
    if (Object.keys(found).length > 0) return;
    onSubmit(draftToUpsertBody(candidate));
  };

  return (
    <form onSubmit={handleSubmit} className="flex min-h-0 flex-1 flex-col">
      <SheetHeader className="px-6 pt-6 pb-0">
        <SheetTitle className="text-lg font-semibold">
          {mode === "create"
            ? "Add scan target"
            : `Edit ${initialDraft.displayName}`}
        </SheetTitle>
        <SheetDescription>
          Every enrolled device agent receives this catalog on its next policy
          poll and probes for the target on its next scan. Signatures are
          matched locally; nothing but the match is reported.
        </SheetDescription>
      </SheetHeader>

      <div className="min-h-0 flex-1 overflow-y-auto px-6 py-6">
        <FieldGroup>
          <Field>
            <FieldLabel htmlFor="ai-scan-target-name">Name</FieldLabel>
            <Input
              id="ai-scan-target-name"
              value={draft.displayName}
              onChange={(value) => update("displayName", value)}
              placeholder="ChatGPT Classic"
              error={
                errors.displayName !== undefined || errors.id !== undefined
              }
            />
            <FieldDescription>{idDescription(id, mode)}</FieldDescription>
            <FieldError>{errors.displayName ?? errors.id}</FieldError>
          </Field>

          <Field>
            <FieldLabel htmlFor="ai-scan-target-category">Category</FieldLabel>
            <Select
              value={draft.category}
              onValueChange={(value) =>
                update("category", value as TargetCategory)
              }
            >
              <SelectTrigger id="ai-scan-target-category" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {TARGET_CATEGORIES.map((option) => (
                  <SelectItem
                    key={option.value}
                    value={option.value}
                    description={option.description}
                  >
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>

          <SignatureField
            id="ai-scan-target-binaries"
            label="Binaries"
            description="Bare command names resolved on the device PATH. Never a path. Installed signal. Press comma after each one."
            placeholder="claude"
            value={draft.binaries}
            error={errors.binaries}
            onChange={(value) => update("binaries", value)}
          />
          <SignatureField
            id="ai-scan-target-config-dirs"
            label="Config dirs"
            description="Home-relative directories whose existence marks the tool as installed. Only existence is checked. Press comma after each one."
            placeholder="~/.claude"
            value={draft.configDirs}
            error={errors.configDirs}
            onChange={(value) => update("configDirs", value)}
          />
          <SignatureField
            id="ai-scan-target-process-names"
            label="Process names"
            description="Exact process names checked for the running signal. Both ChatGPT apps run as “ChatGPT”, so leave this empty when a name cannot tell targets apart. Press comma after each one."
            placeholder="Cursor"
            value={draft.processNames}
            error={errors.processNames}
            onChange={(value) => update("processNames", value)}
          />

          {serverError ? (
            <Text role="alert" className="text-destructive text-sm">
              {serverError}
            </Text>
          ) : null}
        </FieldGroup>
      </div>

      <SheetFooter className="flex-row items-center justify-end gap-2 border-t px-6 py-4">
        <Button type="button" variant="secondary" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" disabled={pending}>
          {mode === "create" ? "Add target" : "Save changes"}
        </Button>
      </SheetFooter>
    </form>
  );
}

type DeleteDialogProps = {
  targetId: string | null;
  pending: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: (targetId: string) => void;
};

export function DeleteAiScanTargetDialog({
  targetId,
  pending,
  onOpenChange,
  onConfirm,
}: DeleteDialogProps): JSX.Element {
  return (
    <Dialog open={targetId !== null} onOpenChange={onOpenChange}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Delete {targetId ?? "target"}</Dialog.Title>
          <Dialog.Description>
            Agents stop probing for this target on their next policy poll.
            Detections already recorded keep the id, and the id can be added
            again later.
          </Dialog.Description>
        </Dialog.Header>
        <Dialog.Footer>
          <Button
            type="button"
            variant="secondary"
            onClick={() => onOpenChange(false)}
          >
            Cancel
          </Button>
          <Button
            type="button"
            variant="destructive-primary"
            disabled={pending || targetId === null}
            onClick={() => {
              if (targetId !== null) onConfirm(targetId);
            }}
          >
            Delete target
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
