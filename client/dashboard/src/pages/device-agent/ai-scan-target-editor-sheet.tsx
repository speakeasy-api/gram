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
import { TextArea } from "@/components/ui/Textarea";
import { Text } from "@/components/ui/Text";
import type { VerifyCimdURLResult } from "@gram/client/models/components/verifycimdurlresult.js";
import { useVerifyUserSessionIssuerCimdClientURLMutation } from "@gram/client/react-query/verifyUserSessionIssuerCimdClientURL.js";
import type { UpsertAiScanTargetRequestBody } from "@gram/client/models/components/upsertaiscantargetrequestbody.js";
import { useState } from "react";
import {
  callsGateway,
  categoryLabel,
  clientIdFromCimdInput,
  draftToUpsertBody,
  normalizeConfigDir,
  slugFromName,
  TARGET_CATEGORIES,
  validateDraft,
  type Draft,
  type DraftErrors,
  type TargetCategory,
} from "./ai-scan-target-draft";

// "view" is a built-in opened for reading. Built-ins are system-supplied and
// read-only — the server refuses a write that would change one — so the sheet
// must not present fields that look editable and then fail on save.
export type EditorMode = "create" | "edit" | "view";

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
  onSubmit: (body: UpsertAiScanTargetRequestBody) => void;
};

// ReadOnlyList renders what a built-in matches on without offering a control
// that would imply it can be changed.
function ReadOnlyList({
  label,
  description,
  value,
  empty,
}: {
  label: string;
  description: string;
  value: string[];
  empty: string;
}): JSX.Element {
  return (
    <Field>
      <FieldLabel>{label}</FieldLabel>
      {value.length === 0 ? (
        <Text muted small>
          {empty}
        </Text>
      ) : (
        <div className="flex flex-wrap gap-1">
          {value.map((entry) => (
            <span
              key={entry}
              className="bg-muted/60 rounded px-2 py-0.5 font-mono text-xs"
            >
              {entry}
            </span>
          ))}
        </div>
      )}
      <FieldDescription>{description}</FieldDescription>
    </Field>
  );
}

function SignatureField({
  id,
  label,
  description,
  placeholder,
  value,
  error,
  separateOnSpace,
  onChange,
}: {
  id: string;
  label: string;
  description: string;
  placeholder: string;
  value: string[];
  error?: string;
  separateOnSpace?: boolean;
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
        separateOnSpace={separateOnSpace}
      />
      <FieldDescription>{description}</FieldDescription>
      <FieldError>{error}</FieldError>
    </Field>
  );
}

/**
 * CimdDocumentsField collects the documents a target publishes — the only
 * thing a block can be enforced on.
 *
 * Two ways into the same box: paste the JSON, which works for a vendor Gram
 * cannot reach, or fetch it from its URL through the endpoint that owns the
 * SSRF policy and rate limit. Only the client_id is stored; a document's
 * client_id equals the URL it is served from, so the two are one fact.
 */
function CimdDocumentsField({
  value,
  error,
  onChange,
}: {
  value: string[];
  error?: string;
  onChange: (value: string[]) => void;
}): JSX.Element {
  const [url, setUrl] = useState("");
  const [documentJson, setDocument] = useState("");
  const [problem, setProblem] = useState<string | null>(null);
  const [probed, setProbed] = useState<VerifyCimdURLResult | null>(null);
  const verify = useVerifyUserSessionIssuerCimdClientURLMutation();

  const fetchFromURL = (): void => {
    const trimmed = url.trim();
    setProbed(null);
    if (!trimmed.startsWith("https://")) {
      setProblem("Enter an https URL to a client ID metadata document");
      return;
    }
    setProblem(null);
    verify.mutate(
      { request: { verifyURLRequestBody: { clientIdMetadataUri: trimmed } } },
      {
        onSuccess: (result) => {
          setProbed(result);
          // Only a verified probe carries a document. An unreachable or
          // rejected one leaves whatever is in the box alone rather than
          // clearing work the operator may have pasted.
          if (result.document) setDocument(result.document);
        },
        onError: () =>
          setProblem("The document could not be fetched. Try again."),
      },
    );
  };

  const add = (): void => {
    const parsed = clientIdFromCimdInput(documentJson);
    if ("error" in parsed) {
      setProblem(parsed.error);
      return;
    }
    if (value.includes(parsed.clientId)) {
      setProblem("That document is already listed");
      return;
    }
    setProblem(null);
    setProbed(null);
    onChange([...value, parsed.clientId]);
    setUrl("");
    setDocument("");
  };

  return (
    <Field>
      <FieldLabel htmlFor="ai-scan-target-cimd-document">
        Client ID metadata documents
      </FieldLabel>
      <div className="flex flex-col gap-3">
        {value.length > 0 ? (
          <ul className="flex flex-col gap-1">
            {value.map((clientId) => (
              <li
                key={clientId}
                className="bg-muted/40 flex items-center justify-between gap-2 rounded px-2 py-1"
              >
                <Text small className="truncate font-mono text-xs">
                  {clientId}
                </Text>
                <Button
                  type="button"
                  variant="tertiary"
                  size="sm"
                  aria-label={`Remove ${clientId}`}
                  onClick={() => onChange(value.filter((v) => v !== clientId))}
                >
                  Remove
                </Button>
              </li>
            ))}
          </ul>
        ) : null}

        <div className="flex items-start gap-2">
          <Input
            id="ai-scan-target-cimd-url"
            value={url}
            onChange={(next: string) => {
              setUrl(next);
              setProbed(null);
              setProblem(null);
            }}
            placeholder="https://vendor.example/oauth/client-metadata.json"
          />
          <Button
            type="button"
            variant="secondary"
            onClick={fetchFromURL}
            disabled={url.trim() === "" || verify.isPending}
          >
            {verify.isPending ? "Fetching…" : "Fetch"}
          </Button>
        </div>

        {/* What the probe made of the URL. Shown whether or not it verified:
            a rejected document is the answer to the operator's question, not
            an error in asking it. */}
        {probed ? (
          <div className="bg-muted/40 flex flex-col gap-1 rounded p-3">
            <Text small className="font-medium">
              {probed.verified
                ? `Fetched${probed.clientName ? `: ${probed.clientName}` : ""}`
                : "Gram could not use this document"}
            </Text>
            <Text muted small className="text-xs">
              {probed.detail}
            </Text>
          </div>
        ) : null}

        <TextArea
          id="ai-scan-target-cimd-document"
          value={documentJson}
          onChange={setDocument}
          rows={8}
          className="font-mono text-xs"
          placeholder={
            '{\n  "client_id": "https://vendor.example/oauth/client-metadata.json",\n  "client_name": "Vendor",\n  "redirect_uris": ["https://vendor.example/callback"]\n}'
          }
        />
        <div>
          <Button
            type="button"
            size="sm"
            onClick={add}
            disabled={documentJson.trim() === ""}
          >
            Add document
          </Button>
        </div>
      </div>
      <FieldDescription>
        Paste the document, or fetch it from its URL. Gram stores its client_id,
        which a document must set to the URL it is served from. A tool that
        publishes no document cannot be blocked at the gateway, and the
        inventory leaves it unreviewed.
      </FieldDescription>
      <FieldError>{problem ?? error}</FieldError>
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
  onSubmit: (body: UpsertAiScanTargetRequestBody) => void;
}): JSX.Element {
  const [draft, setDraft] = useState<Draft>(initialDraft);
  const [errors, setErrors] = useState<DraftErrors>({});
  const update = <K extends keyof Draft>(key: K, value: Draft[K]): void => {
    setDraft((current) => ({ ...current, [key]: value }));
  };

  const id = mode === "create" ? slugFromName(draft.displayName) : draft.id;
  const readOnly = mode === "view";

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
          {mode === "create" ? "Add scan target" : initialDraft.displayName}
        </SheetTitle>
        <SheetDescription>
          {readOnly
            ? "A built-in target, supplied and kept current by Speakeasy. It cannot be edited — switch it off from the row menu if this organization should not probe for it."
            : "Every enrolled device agent receives this library on its next policy poll and probes for the target on its next scan. Signatures are matched locally; nothing but the match is reported."}
        </SheetDescription>
      </SheetHeader>

      <div className="min-h-0 flex-1 overflow-y-auto px-6 py-6">
        <FieldGroup>
          {readOnly ? (
            <Field>
              <FieldLabel>Category</FieldLabel>
              <Text small>{categoryLabel(draft.category)}</Text>
              <FieldDescription>
                Agents report this target as{" "}
                <span className="font-mono">{id}</span>.
              </FieldDescription>
            </Field>
          ) : (
            <>
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
                <FieldLabel htmlFor="ai-scan-target-category">
                  Category
                </FieldLabel>
                <Select
                  value={draft.category}
                  onValueChange={(value) =>
                    update("category", value as TargetCategory)
                  }
                >
                  <SelectTrigger
                    id="ai-scan-target-category"
                    className="w-full"
                  >
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
            </>
          )}

          {readOnly ? (
            <>
              <ReadOnlyList
                label="Binaries"
                description="Bare command names resolved on the device PATH. Installed signal."
                value={draft.binaries}
                empty="None"
              />
              <ReadOnlyList
                label="Config dirs"
                description="Directories whose existence marks the tool as installed."
                value={draft.configDirs}
                empty="None"
              />
              <ReadOnlyList
                label="Process names"
                description="Exact process names checked for the running signal."
                value={draft.processNames}
                empty="None"
              />
              {callsGateway(draft.category) ? (
                <ReadOnlyList
                  label="Client ID metadata documents"
                  description="The documents this tool publishes. A block reaches the gateway only through one of these; a tool with none is recorded as blocked and not enforced."
                  value={draft.oauthClientIds}
                  empty="None — a block on this tool cannot be enforced at the gateway."
                />
              ) : null}
            </>
          ) : (
            <>
              <SignatureField
                id="ai-scan-target-binaries"
                label="Binaries"
                description="Bare command names resolved on the device PATH. Never a path. Installed signal. Press comma or space after each one."
                placeholder="claude"
                separateOnSpace
                value={draft.binaries}
                error={errors.binaries}
                onChange={(value) => update("binaries", value)}
              />
              <SignatureField
                id="ai-scan-target-config-dirs"
                label="Config dirs"
                description="Directories whose existence marks the tool as installed, inside the home folder unless they start with /. Only existence is checked. Press comma after each one."
                placeholder=".claude"
                value={draft.configDirs}
                error={errors.configDirs}
                onChange={(value) =>
                  update(
                    "configDirs",
                    Array.from(
                      new Set(value.map(normalizeConfigDir).filter(Boolean)),
                    ),
                  )
                }
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

              {/* A harness and an assistant both reach Gram's MCP gateway.
                  An open model runtime is software on a laptop and nothing
                  else, so there is no caller to recognise and the whole block
                  is hidden. */}
              {callsGateway(draft.category) ? (
                <>
                  <CimdDocumentsField
                    value={draft.oauthClientIds}
                    error={errors.oauthClientIds}
                    onChange={(value) => update("oauthClientIds", value)}
                  />
                  <SignatureField
                    id="ai-scan-target-client-info-names"
                    label="Reported client names"
                    description="Names the client reports when it connects. Detection only — never used to allow or block, because the value is self-reported. Press comma after each one."
                    placeholder="claude-code"
                    value={draft.clientInfoNames}
                    error={errors.clientInfoNames}
                    onChange={(value) => update("clientInfoNames", value)}
                  />
                </>
              ) : null}
            </>
          )}

          {serverError ? (
            <Text role="alert" className="text-destructive text-sm">
              {serverError}
            </Text>
          ) : null}
        </FieldGroup>
      </div>

      <SheetFooter className="flex-row items-center justify-end gap-2 border-t px-6 py-4">
        <Button type="button" variant="secondary" onClick={onCancel}>
          {readOnly ? "Close" : "Cancel"}
        </Button>
        {readOnly ? null : (
          <Button type="submit" disabled={pending}>
            {mode === "create" ? "Add target" : "Save changes"}
          </Button>
        )}
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
