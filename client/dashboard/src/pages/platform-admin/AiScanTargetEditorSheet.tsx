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
import { Switch } from "@/components/ui/Switch";
import { Text } from "@/components/ui/Text";
import type { UpsertRequestBody2 } from "@gram/client/models/components/upsertrequestbody2.js";
import { useState } from "react";
import {
  draftToUpsertBody,
  TARGET_CATEGORIES,
  validateDraft,
  type Draft,
  type DraftErrors,
  type TargetCategory,
} from "./aiScanTargetDraft";

export type EditorMode = "create" | "edit";

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
  value: string;
  error?: string;
  onChange: (value: string) => void;
}): JSX.Element {
  return (
    <Field>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Input
        id={id}
        lines={3}
        value={value}
        onChange={onChange}
        placeholder={placeholder}
        error={error !== undefined}
        className="font-mono text-xs"
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
      <SheetContent className="flex w-full flex-col gap-0 overflow-y-auto sm:max-w-xl">
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

  const handleSubmit = (event: React.FormEvent<HTMLFormElement>): void => {
    event.preventDefault();
    const found = validateDraft(draft);
    setErrors(found);
    if (Object.keys(found).length > 0) return;
    onSubmit(draftToUpsertBody(draft));
  };

  return (
    <form onSubmit={handleSubmit} className="flex flex-1 flex-col">
      <SheetHeader>
        <SheetTitle>
          {mode === "create" ? "Add scan target" : `Edit ${initialDraft.id}`}
        </SheetTitle>
        <SheetDescription>
          Every enrolled device agent receives this catalog on its next policy
          poll and probes for the target on its next scan. Signatures are
          matched locally; nothing but the match is reported.
        </SheetDescription>
      </SheetHeader>

      <FieldGroup className="flex-1 space-y-5 py-4">
        <Field>
          <FieldLabel htmlFor="ai-scan-target-id">Id</FieldLabel>
          <Input
            id="ai-scan-target-id"
            value={draft.id}
            onChange={(value) => update("id", value)}
            disabled={mode === "edit"}
            placeholder="chatgpt-classic"
            error={errors.id !== undefined}
            className="font-mono"
          />
          <FieldDescription>
            Stable identifier agents report and detections key on. Never reused
            for a different tool.
          </FieldDescription>
          <FieldError>{errors.id}</FieldError>
        </Field>

        <Field>
          <FieldLabel htmlFor="ai-scan-target-name">Display name</FieldLabel>
          <Input
            id="ai-scan-target-name"
            value={draft.displayName}
            onChange={(value) => update("displayName", value)}
            placeholder="ChatGPT Classic"
            error={errors.displayName !== undefined}
          />
          <FieldError>{errors.displayName}</FieldError>
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
          id="ai-scan-target-bundle-ids"
          label="Bundle ids"
          description="macOS CFBundleIdentifier values matched against app bundles under /Applications and ~/Applications. Installed signal, with the version read from Info.plist."
          placeholder="com.openai.chat"
          value={draft.bundleIds}
          error={errors.bundleIds}
          onChange={(value) => update("bundleIds", value)}
        />
        <SignatureField
          id="ai-scan-target-binaries"
          label="Binaries"
          description="Bare command names resolved on the device PATH. Never a path. Installed signal."
          placeholder="claude"
          value={draft.binaries}
          error={errors.binaries}
          onChange={(value) => update("binaries", value)}
        />
        <SignatureField
          id="ai-scan-target-config-dirs"
          label="Config dirs"
          description="Home-relative directories whose existence marks the tool as installed. Only existence is checked."
          placeholder="~/.claude"
          value={draft.configDirs}
          error={errors.configDirs}
          onChange={(value) => update("configDirs", value)}
        />
        <SignatureField
          id="ai-scan-target-process-names"
          label="Process names"
          description="Exact process names checked for the running signal. Both ChatGPT apps run as “ChatGPT”, so leave this empty when a name cannot tell targets apart."
          placeholder="Cursor"
          value={draft.processNames}
          error={errors.processNames}
          onChange={(value) => update("processNames", value)}
        />

        <Field>
          <FieldLabel htmlFor="ai-scan-target-plist-key">
            Version plist key
          </FieldLabel>
          <Input
            id="ai-scan-target-plist-key"
            value={draft.versionPlistKey}
            onChange={(value) => update("versionPlistKey", value)}
            placeholder="CFBundleShortVersionString"
            error={errors.versionPlistKey !== undefined}
            className="font-mono"
          />
          <FieldDescription>
            Optional Info.plist key to read the installed version from on a
            bundle match. Defaults to CFBundleShortVersionString.
          </FieldDescription>
          <FieldError>{errors.versionPlistKey}</FieldError>
        </Field>

        <Field>
          <div className="flex items-center justify-between gap-4">
            <div>
              <FieldLabel id="ai-scan-target-enabled-label">
                Served to agents
              </FieldLabel>
              <FieldDescription>
                Disabled targets stay in the catalog for history but are not
                probed for.
              </FieldDescription>
            </div>
            <Switch
              checked={draft.enabled}
              onCheckedChange={(checked) => update("enabled", checked)}
              aria-labelledby="ai-scan-target-enabled-label"
            />
          </div>
        </Field>

        <Field>
          <FieldLabel htmlFor="ai-scan-target-reason">Reason</FieldLabel>
          <Input
            id="ai-scan-target-reason"
            value={draft.reason}
            onChange={(value) => update("reason", value)}
            placeholder="Why this change is being made"
            error={errors.reason !== undefined}
          />
          <FieldDescription>
            Recorded on the catalog revision alongside your user.
          </FieldDescription>
          <FieldError>{errors.reason}</FieldError>
        </Field>

        {serverError ? (
          <Text role="alert" className="text-destructive text-sm">
            {serverError}
          </Text>
        ) : null}
      </FieldGroup>

      <SheetFooter>
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
