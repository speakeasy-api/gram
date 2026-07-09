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
import type { HeaderInput } from "@gram/client/models/components/headerinput.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { RemoteMcpServerHeader } from "@gram/client/models/components/remotemcpserverheader.js";
import {
  invalidateAllGetRemoteMcpServer,
  useGetRemoteMcpServer,
} from "@gram/client/react-query/getRemoteMcpServer.js";
import { invalidateAllRemoteMcpServers } from "@gram/client/react-query/remoteMcpServers.js";
import { useUpdateRemoteMcpServerMutation } from "@gram/client/react-query/updateRemoteMcpServer.js";
import { Alert, Button, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2, Plus, Trash2 } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { FooterSaveButtonContent, SettingsSection } from "../SettingsSection";

const REDACTED_SECRET = "***";

type HeaderSource = "static" | "environment" | "request";

type HeaderDraft = {
  key: string;
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

function headerDraftToInput(draft: HeaderDraft): HeaderInput {
  const base: HeaderInput = {
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

function draftsEqual(a: HeaderDraft[], b: HeaderDraft[]): boolean {
  if (a.length !== b.length) return false;
  for (let index = 0; index < a.length; index += 1) {
    const draft = a[index];
    const other = b[index];
    if (!draft || !other) return false;
    if (
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

export function HeadersSection({
  mcpServer,
}: {
  mcpServer: McpServer;
}): JSX.Element | null {
  if (!mcpServer.remoteMcpServerId) {
    return null;
  }

  return (
    <HeadersSectionContent remoteMcpServerId={mcpServer.remoteMcpServerId} />
  );
}

function HeadersSectionContent({
  remoteMcpServerId,
}: {
  remoteMcpServerId: string;
}): JSX.Element {
  const remoteMcpQuery = useGetRemoteMcpServer(
    { id: remoteMcpServerId },
    undefined,
    { enabled: remoteMcpServerId !== "" },
  );
  const initialHeaders = remoteMcpQuery.data?.headers ?? [];

  const initialDrafts = useMemo(
    () => initialHeaders.map(headerDraftFromServer),
    [initialHeaders],
  );
  const [drafts, setDrafts] = useState(initialDrafts);

  useEffect(() => {
    setDrafts(initialDrafts);
  }, [initialDrafts]);

  const queryClient = useQueryClient();
  const update = useUpdateRemoteMcpServerMutation();

  const validationError = validateDrafts(drafts);
  const dirty = !draftsEqual(drafts, initialDrafts);
  const isSaving = update.isPending;
  const saveDisabled =
    !dirty || isSaving || validationError !== null || remoteMcpQuery.isLoading;

  const handleSave = async () => {
    if (validationError) return;
    try {
      await update.mutateAsync({
        request: {
          updateServerForm: {
            id: remoteMcpServerId,
            headers: drafts.map(headerDraftToInput),
          },
        },
      });
      await Promise.all([
        invalidateAllGetRemoteMcpServer(queryClient, { refetchType: "all" }),
        invalidateAllRemoteMcpServers(queryClient, { refetchType: "all" }),
      ]);
      toast.success("Upstream headers updated");
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "Failed to update headers";
      toast.error(message);
    }
  };

  return (
    <SettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>Upstream Headers</SettingsSection.Title>
        <SettingsSection.Description>
          Headers sent to the remote MCP URL. Choose &ldquo;From
          environment&rdquo; to fill the value from the environment attached
          above (header name must match the environment variable).
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          {remoteMcpQuery.isLoading ? (
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

          {update.isError ? (
            <Alert variant="error" dismissible={false}>
              {update.error.message}
            </Alert>
          ) : null}

          <RequireScope scope="mcp:write" level="component">
            <Button
              variant="secondary"
              size="md"
              disabled={remoteMcpQuery.isLoading}
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
        </SettingsSection.Body>
        <SettingsSection.Footer>
          <SettingsSection.FooterHint>
            Static secrets are redacted after save. Leave the value blank to
            keep the current secret.
          </SettingsSection.FooterHint>
          <SettingsSection.FooterActions>
            <RequireScope scope="mcp:write" level="component">
              <Button
                variant="primary"
                size="md"
                disabled={saveDisabled}
                onClick={() => void handleSave()}
              >
                <FooterSaveButtonContent pending={isSaving} />
              </Button>
            </RequireScope>
          </SettingsSection.FooterActions>
        </SettingsSection.Footer>
      </SettingsSection.Panel>
    </SettingsSection>
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
