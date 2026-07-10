import { RequireScope } from "@/components/require-scope";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Type } from "@/components/ui/type";
import type { RemoteMcpServerHeader } from "@gram/client/models/components/remotemcpserverheader.js";
import { useCreateRemoteMcpServerHeaderMutation } from "@gram/client/react-query/createRemoteMcpServerHeader.js";
import { useDeleteRemoteMcpServerHeaderMutation } from "@gram/client/react-query/deleteRemoteMcpServerHeader.js";
import {
  invalidateAllRemoteMcpServerHeaders,
  useRemoteMcpServerHeaders,
} from "@gram/client/react-query/remoteMcpServerHeaders.js";
import { useUpdateRemoteMcpServerHeaderMutation } from "@gram/client/react-query/updateRemoteMcpServerHeader.js";
import { Alert, Button, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2, Plus, Trash2 } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";

const REDACTED_SECRET = "***";

type HeaderSource = "static" | "environment" | "request";

type HeaderDraft = {
  key: string;
  /** Set for headers that already exist on the server. */
  id?: string;
  name: string;
  source: HeaderSource;
  staticValue: string;
  valueFromRequestHeader: string;
  isRequired: boolean;
  isSecret: boolean;
  hadSecret: boolean;
};

function headerSourceFromServer(header: RemoteMcpServerHeader): HeaderSource {
  if (header.valueFromRequestHeader) {
    return "request";
  }
  if (header.value === "") {
    return "environment";
  }
  return "static";
}

function headerDraftFromServer(header: RemoteMcpServerHeader): HeaderDraft {
  const source = headerSourceFromServer(header);
  const isRedactedSecret = header.isSecret && header.value === REDACTED_SECRET;

  return {
    key: header.id,
    id: header.id,
    name: header.name,
    source,
    staticValue:
      source === "static" && !isRedactedSecret ? (header.value ?? "") : "",
    valueFromRequestHeader: header.valueFromRequestHeader ?? "",
    isRequired: header.isRequired,
    isSecret: header.isSecret,
    hadSecret: header.isSecret,
  };
}

function draftsEqual(a: HeaderDraft[], b: HeaderDraft[]): boolean {
  if (a.length !== b.length) return false;
  for (let index = 0; index < a.length; index += 1) {
    const draft = a[index];
    const other = b[index];
    if (!draft || !other) return false;
    if (
      draft.id !== other.id ||
      draft.name !== other.name ||
      draft.source !== other.source ||
      draft.staticValue !== other.staticValue ||
      draft.valueFromRequestHeader !== other.valueFromRequestHeader ||
      draft.isRequired !== other.isRequired ||
      draft.isSecret !== other.isSecret
    ) {
      return false;
    }
  }
  return true;
}

function validateDrafts(drafts: HeaderDraft[]): string | null {
  const names = new Set<string>();
  for (const draft of drafts) {
    const name = draft.name.trim();
    if (!name) {
      return "Every header needs a name.";
    }
    const normalized = name.toLowerCase();
    if (names.has(normalized)) {
      return `Duplicate header name "${name}".`;
    }
    names.add(normalized);

    if (draft.source === "request" && !draft.valueFromRequestHeader.trim()) {
      return `Header "${name}" needs an inbound request header name.`;
    }

    if (draft.source === "static") {
      const needsValue =
        !draft.isSecret || !draft.hadSecret || draft.staticValue.trim() !== "";
      if (needsValue && draft.staticValue.trim() === "") {
        return `Header "${name}" needs a static value.`;
      }
    }
  }

  return null;
}

function newHeaderDraft(): HeaderDraft {
  return {
    key: crypto.randomUUID(),
    name: "",
    source: "environment",
    staticValue: "",
    valueFromRequestHeader: "",
    isRequired: false,
    isSecret: false,
    hadSecret: false,
  };
}

type HeaderWriteFields = {
  name: string;
  isRequired: boolean;
  isSecret?: boolean;
  value?: string;
  valueFromRequestHeader?: string;
};

function headerDraftToWriteFields(draft: HeaderDraft): HeaderWriteFields {
  const base = {
    name: draft.name.trim(),
    isRequired: draft.isRequired,
  };

  if (draft.source === "request") {
    return {
      ...base,
      isSecret: false,
      valueFromRequestHeader: draft.valueFromRequestHeader.trim(),
    };
  }

  if (draft.source === "environment") {
    // Empty non-null value is the env-source sentinel (ADR-0002).
    return {
      ...base,
      isSecret: false,
      value: "",
    };
  }

  const trimmedValue = draft.staticValue.trim();
  if (draft.isSecret && trimmedValue === "" && draft.hadSecret) {
    return {
      ...base,
      isSecret: true,
    };
  }

  return {
    ...base,
    isSecret: draft.isSecret,
    value: draft.staticValue,
  };
}

export function HeadersSection({
  remoteMcpServerId,
}: {
  remoteMcpServerId: string;
}): JSX.Element {
  const headersQuery = useRemoteMcpServerHeaders(
    { remoteMcpServerId },
    undefined,
    { enabled: remoteMcpServerId !== "" },
  );
  const headers = headersQuery.data?.headers;

  const initialDrafts = useMemo(
    () => (headers ?? []).map(headerDraftFromServer),
    [headers],
  );
  const [drafts, setDrafts] = useState(initialDrafts);
  // The server snapshot the current drafts were last synced from. Used to tell
  // "user hasn't touched anything" apart from "user has unsaved edits" so a
  // background refetch (window refocus, concurrent change) can't silently
  // discard in-progress edits.
  const syncedRef = useRef(initialDrafts);

  useEffect(() => {
    const previousSynced = syncedRef.current;
    syncedRef.current = initialDrafts;
    setDrafts((current) =>
      draftsEqual(current, previousSynced) ? initialDrafts : current,
    );
  }, [initialDrafts]);

  const queryClient = useQueryClient();
  const createHeader = useCreateRemoteMcpServerHeaderMutation();
  const updateHeader = useUpdateRemoteMcpServerHeaderMutation();
  const deleteHeader = useDeleteRemoteMcpServerHeaderMutation();

  const validationError = validateDrafts(drafts);
  const dirty = !draftsEqual(drafts, initialDrafts);
  const saving =
    createHeader.isPending || updateHeader.isPending || deleteHeader.isPending;
  const saveDisabled =
    !dirty || saving || validationError !== null || headersQuery.isLoading;

  const handleSave = async () => {
    if (validationError) return;

    const initialById = new Map(
      initialDrafts
        .filter((draft): draft is HeaderDraft & { id: string } => !!draft.id)
        .map((draft) => [draft.id, draft]),
    );
    const keptIds = new Set(
      drafts.flatMap((draft) => (draft.id ? [draft.id] : [])),
    );

    try {
      for (const draft of initialDrafts) {
        if (draft.id && !keptIds.has(draft.id)) {
          await deleteHeader.mutateAsync({
            request: { id: draft.id },
          });
        }
      }

      for (const draft of drafts) {
        const fields = headerDraftToWriteFields(draft);
        if (!draft.id) {
          await createHeader.mutateAsync({
            request: {
              createServerHeaderForm: {
                remoteMcpServerId,
                ...fields,
              },
            },
          });
          continue;
        }

        const initial = initialById.get(draft.id);
        if (!initial || draftsEqual([draft], [initial])) {
          continue;
        }

        await updateHeader.mutateAsync({
          request: {
            updateServerHeaderForm: {
              id: draft.id,
              ...fields,
            },
          },
        });
      }

      await invalidateAllRemoteMcpServerHeaders(queryClient, {
        refetchType: "all",
      });
      // Intentionally adopt the canonical server state after a save so drafts
      // pick up server-assigned ids and secret redaction. The sync effect
      // preserves unsaved edits, so this reset must be explicit.
      const refreshed = await headersQuery.refetch();
      const synced = (refreshed.data?.headers ?? []).map(headerDraftFromServer);
      syncedRef.current = synced;
      setDrafts(synced);
      toast.success("Upstream headers updated");
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "Failed to update headers";
      toast.error(message);
      await invalidateAllRemoteMcpServerHeaders(queryClient, {
        refetchType: "all",
      });
    }
  };

  const mutationError =
    createHeader.error ?? updateHeader.error ?? deleteHeader.error;

  return (
    <div className="rounded-lg border p-6">
      <Type variant="subheading" className="mb-1">
        Upstream Headers
      </Type>
      <Type muted small className="mb-4">
        Headers sent to the remote MCP URL. Choose &ldquo;From
        environment&rdquo; to fill the value from the environment attached to an
        MCP server that fronts this source (header name must match the
        environment variable).
      </Type>
      <Stack gap={4}>
        {headersQuery.isLoading ? (
          <Type muted small>
            Loading headers…
          </Type>
        ) : drafts.length === 0 ? (
          <Type muted small>
            No upstream headers configured yet.
          </Type>
        ) : (
          <Stack gap={4}>
            {drafts.map((draft, index) => (
              <HeaderDraftRow
                key={draft.key}
                draft={draft}
                onChange={(next) =>
                  setDrafts((current) =>
                    current.map((row, rowIndex) =>
                      rowIndex === index ? next : row,
                    ),
                  )
                }
                onRemove={() =>
                  setDrafts((current) =>
                    current.filter((_, rowIndex) => rowIndex !== index),
                  )
                }
              />
            ))}
          </Stack>
        )}

        {validationError && dirty ? (
          <Type small className="text-destructive">
            {validationError}
          </Type>
        ) : null}

        {mutationError ? (
          <Alert variant="error" dismissible={false}>
            {mutationError.message}
          </Alert>
        ) : null}

        <RequireScope scope="mcp:write" level="component">
          <Button
            variant="secondary"
            size="md"
            disabled={headersQuery.isLoading}
            onClick={() =>
              setDrafts((current) => [...current, newHeaderDraft()])
            }
          >
            <Button.LeftIcon>
              <Plus className="size-4" />
            </Button.LeftIcon>
            <Button.Text>Add header</Button.Text>
          </Button>
        </RequireScope>

        <Type muted small>
          Static secrets are redacted after save. Leave the value blank to keep
          the current secret.
        </Type>

        <Stack direction="horizontal" gap={2}>
          <RequireScope scope="mcp:write" level="component">
            <Button
              variant="primary"
              size="md"
              disabled={saveDisabled}
              onClick={() => void handleSave()}
            >
              {saving ? (
                <>
                  <Button.LeftIcon>
                    <Loader2 className="size-4 animate-spin" />
                  </Button.LeftIcon>
                  <Button.Text>Saving</Button.Text>
                </>
              ) : (
                <Button.Text>Save</Button.Text>
              )}
            </Button>
          </RequireScope>
        </Stack>
      </Stack>
    </div>
  );
}

function HeaderDraftRow({
  draft,
  onChange,
  onRemove,
}: {
  draft: HeaderDraft;
  onChange: (draft: HeaderDraft) => void;
  onRemove: () => void;
}): JSX.Element {
  const showSecretPlaceholder =
    draft.source === "static" &&
    draft.isSecret &&
    draft.hadSecret &&
    draft.staticValue === "";

  return (
    <div className="rounded-md border p-4">
      <Stack gap={3}>
        <Stack direction="horizontal" gap={3} align="start">
          <div className="min-w-0 flex-1">
            <Type small muted className="mb-1">
              Header name
            </Type>
            <Input
              value={draft.name}
              onChange={(value) => onChange({ ...draft, name: value })}
              placeholder="Authorization"
            />
          </div>
          <div className="w-52 shrink-0">
            <Type small muted className="mb-1">
              Value source
            </Type>
            <Select
              value={draft.source}
              onValueChange={(value) => {
                const source = value as HeaderSource;
                onChange({
                  ...draft,
                  source,
                  isSecret: source === "static" ? draft.isSecret : false,
                });
              }}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="environment">From environment</SelectItem>
                <SelectItem value="static">Static value</SelectItem>
                <SelectItem value="request">From request header</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <RequireScope scope="mcp:write" level="component">
            <Button
              variant="tertiary"
              size="md"
              className="mt-6 shrink-0"
              onClick={onRemove}
              aria-label={`Remove header ${draft.name || "row"}`}
            >
              <Button.LeftIcon>
                <Trash2 className="size-4" />
              </Button.LeftIcon>
            </Button>
          </RequireScope>
        </Stack>

        {draft.source === "static" ? (
          <div>
            <Type small muted className="mb-1">
              Static value
            </Type>
            <Input
              value={draft.staticValue}
              onChange={(value) => onChange({ ...draft, staticValue: value })}
              placeholder={
                showSecretPlaceholder
                  ? "Leave blank to keep current secret"
                  : "Bearer …"
              }
              type={draft.isSecret ? "password" : "text"}
            />
          </div>
        ) : null}

        {draft.source === "request" ? (
          <div>
            <Type small muted className="mb-1">
              Inbound request header
            </Type>
            <Input
              value={draft.valueFromRequestHeader}
              onChange={(value) =>
                onChange({ ...draft, valueFromRequestHeader: value })
              }
              placeholder="X-Forwarded-Authorization"
            />
          </div>
        ) : null}

        <Stack direction="horizontal" gap={4}>
          <label className="flex items-center gap-2">
            <Checkbox
              checked={draft.isRequired}
              onCheckedChange={(checked) =>
                onChange({ ...draft, isRequired: checked === true })
              }
            />
            <Type small>Required</Type>
          </label>
          {draft.source === "static" ? (
            <label className="flex items-center gap-2">
              <Checkbox
                checked={draft.isSecret}
                onCheckedChange={(checked) =>
                  onChange({ ...draft, isSecret: checked === true })
                }
              />
              <Type small>Secret</Type>
            </label>
          ) : null}
        </Stack>
      </Stack>
    </div>
  );
}
