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
import { mcpServerRouteParam } from "@/lib/sources";
import { type PulseMCPServer, useListMCPCatalog } from "@/pages/catalog/hooks";
import { catalogHeadersForRemoteUrl } from "@/pages/catalog/remotes";
import { useRoutes } from "@/routes";
import type { ExternalMCPRemoteHeader } from "@gram/client/models/components/externalmcpremoteheader.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { RemoteMcpServerHeader } from "@gram/client/models/components/remotemcpserverheader.js";
import { useCreateRemoteMcpServerHeaderMutation } from "@gram/client/react-query/createRemoteMcpServerHeader.js";
import { useDeleteRemoteMcpServerHeaderMutation } from "@gram/client/react-query/deleteRemoteMcpServerHeader.js";
import { useGetRemoteMcpServer } from "@gram/client/react-query/getRemoteMcpServer.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import {
  invalidateAllRemoteMcpServerHeaders,
  useRemoteMcpServerHeaders,
} from "@gram/client/react-query/remoteMcpServerHeaders.js";
import { useUpdateRemoteMcpServerHeaderMutation } from "@gram/client/react-query/updateRemoteMcpServerHeader.js";
import { Alert, Badge, Button, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { ArrowRight, Eye, EyeOff, Loader2, Plus, Trash2 } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { Link } from "react-router";
import { toast } from "sonner";

const REDACTED_SECRET = "***";

type HeaderSource = "static" | "request";

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
      source === "static"
        ? isRedactedSecret
          ? REDACTED_SECRET
          : (header.value ?? "")
        : "",
    valueFromRequestHeader: header.valueFromRequestHeader ?? "",
    isRequired: header.isRequired,
    isSecret: header.isSecret,
    hadSecret: header.isSecret,
  };
}

// A saved secret shows its redacted placeholder (`***`) in the value field. As
// long as the user leaves that placeholder untouched, we keep the existing
// secret rather than overwriting it with the literal redaction string.
function isUnchangedSecret(draft: HeaderDraft): boolean {
  return (
    draft.isSecret && draft.hadSecret && draft.staticValue === REDACTED_SECRET
  );
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

    if (
      draft.source === "static" &&
      !isUnchangedSecret(draft) &&
      draft.staticValue.trim() === ""
    ) {
      return `Header "${name}" needs a static value.`;
    }
  }

  return null;
}

function newHeaderDraft(): HeaderDraft {
  return {
    key: crypto.randomUUID(),
    name: "",
    source: "static",
    staticValue: "",
    valueFromRequestHeader: "",
    isRequired: false,
    isSecret: true,
    hadSecret: false,
  };
}

function headerDraftFromCatalog(header: ExternalMCPRemoteHeader): HeaderDraft {
  return {
    ...newHeaderDraft(),
    name: header.name,
    isRequired: header.isRequired ?? false,
    // Registries omit is_secret inconsistently; default suggested headers to
    // secret so an API key never lands in plain text by accident.
    isSecret: header.isSecret ?? true,
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

  if (isUnchangedSecret(draft)) {
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

// A single remote_mcps row can back several mcp_servers rows. Its headers are
// stored on the remote, so editing them from any one MCP server silently
// rewrites the values every sibling server sends. HeadersSectionContext tells
// the component which surface it's rendered from so it can guard against that:
//  - "mcp-server": rendered on an MCP server's Settings tab. When the backing
//    remote is shared by more than one server, editing is locked and the user
//    is pointed at the Remote MCP source, the single canonical edit surface.
//  - "remote-mcp": rendered on the Remote MCP source page. Always editable,
//    with an indicator listing every MCP server the change will affect.
export type HeadersSectionContext =
  | { kind: "mcp-server" }
  | { kind: "remote-mcp"; linkedMcpServers: McpServer[] };

export function HeadersSection({
  remoteMcpServerId,
  context,
}: {
  remoteMcpServerId: string;
  context: HeadersSectionContext;
}): JSX.Element {
  const routes = useRoutes();

  // On an MCP server's Settings tab we need to know whether the backing remote
  // is shared before deciding to lock editing. On the Remote MCP page the
  // caller already knows the linked servers, so skip the extra fetch there.
  const isMcpServerContext = context.kind === "mcp-server";
  const siblingsQuery = useMcpServers({ remoteMcpServerId }, undefined, {
    enabled: isMcpServerContext && remoteMcpServerId !== "",
  });
  const linkedMcpServers = useMemo(() => {
    if (context.kind === "remote-mcp") return context.linkedMcpServers;
    return (siblingsQuery.data?.mcpServers ?? []).filter(
      (server) => server.remoteMcpServerId === remoteMcpServerId,
    );
  }, [context, siblingsQuery.data, remoteMcpServerId]);

  const sharedByOthers = linkedMcpServers.length > 1;
  const readOnly = isMcpServerContext && sharedByOthers;
  const siblingsLoading = isMcpServerContext && siblingsQuery.isLoading;

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

  // Catalog-suggested headers: when the remote's URL matches a catalog entry
  // that publishes header requirements (e.g. an API key) and no headers are
  // configured yet, seed the form with those rows so the user only has to fill
  // in values. Suggestions are unsaved drafts — nothing persists until Save.
  const headersEmpty = !!headersQuery.data && initialDrafts.length === 0;
  const suggestionsEnabled = headersEmpty && !readOnly && !siblingsLoading;
  const { data: remoteMcpServer } = useGetRemoteMcpServer(
    { id: remoteMcpServerId },
    undefined,
    { enabled: suggestionsEnabled && remoteMcpServerId !== "" },
  );
  const { data: catalogData } = useListMCPCatalog(
    undefined,
    undefined,
    suggestionsEnabled,
  );
  const suggestedHeaders = useMemo(() => {
    if (!remoteMcpServer?.url || !catalogData?.servers) return [];
    return catalogHeadersForRemoteUrl(
      catalogData.servers as PulseMCPServer[],
      remoteMcpServer.url,
    );
  }, [catalogData?.servers, remoteMcpServer?.url]);

  const suggestedDrafts = useMemo(
    () => suggestedHeaders.map(headerDraftFromCatalog),
    [suggestedHeaders],
  );

  const [suggestionsSeeded, setSuggestionsSeeded] = useState(false);
  useEffect(() => {
    if (!suggestionsEnabled || suggestionsSeeded) return;
    if (suggestedDrafts.length === 0) return;
    setSuggestionsSeeded(true);
    // Only seed a pristine form — never clobber rows the user already added.
    setDrafts((current) => (current.length === 0 ? suggestedDrafts : current));
  }, [suggestionsEnabled, suggestionsSeeded, suggestedDrafts]);

  // Freshly seeded suggestions are intentionally value-less; don't greet the
  // user with a "needs a static value" error before they've touched anything.
  const pristineSuggestions =
    suggestionsSeeded && draftsEqual(drafts, suggestedDrafts);

  const queryClient = useQueryClient();
  const createHeader = useCreateRemoteMcpServerHeaderMutation();
  const updateHeader = useUpdateRemoteMcpServerHeaderMutation();
  const deleteHeader = useDeleteRemoteMcpServerHeaderMutation();

  const validationError = validateDrafts(drafts);
  const dirty = !draftsEqual(drafts, initialDrafts);
  const saving =
    createHeader.isPending || updateHeader.isPending || deleteHeader.isPending;
  const saveDisabled =
    readOnly ||
    !dirty ||
    saving ||
    validationError !== null ||
    headersQuery.isLoading;

  const handleSave = async () => {
    // Defensive: the save/add controls are hidden in read-only mode, but a
    // shared remote must never be mutated from an MCP server's Settings tab.
    if (readOnly || validationError) return;

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
      if (refreshed.isError || !refreshed.data) {
        // Save succeeded, but the refresh failed. Don't treat the missing
        // result as an empty header list — that would wipe the form. Leave the
        // current drafts/cache intact and surface the refresh failure instead.
        toast.success("Upstream headers updated");
        toast.warning("Couldn't refresh headers. Reload to see the latest.");
        return;
      }
      const synced = (refreshed.data.headers ?? []).map(headerDraftFromServer);
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

  const remoteSettingsHref = `${routes.sources.source.href(
    "remotemcp",
    remoteMcpServerId,
  )}#settings`;

  return (
    <div className="rounded-lg border p-6">
      <Type variant="subheading" className="mb-1">
        Upstream Headers
      </Type>
      <Type muted small className="mb-4">
        Headers sent to the remote MCP URL.
      </Type>
      <Stack gap={4}>
        {readOnly ? (
          <Alert variant="warning" dismissible={false}>
            <Stack gap={2}>
              <Type small>
                These headers are shared by {linkedMcpServers.length} MCP
                servers backed by this remote source. Editing them here would
                change the values every one of those servers sends, so editing
                is disabled on this page.
              </Type>
              <Link
                to={remoteSettingsHref}
                className="text-primary inline-flex items-center gap-1 text-sm hover:underline"
              >
                Edit on the Remote MCP source
                <ArrowRight className="size-3.5" />
              </Link>
            </Stack>
          </Alert>
        ) : null}

        {context.kind === "remote-mcp" && linkedMcpServers.length > 0 ? (
          <Alert variant="warning" dismissible={false}>
            <Stack gap={1}>
              <Type small>
                Changes here affect {linkedMcpServers.length}{" "}
                {linkedMcpServers.length === 1 ? "MCP server" : "MCP servers"}{" "}
                backed by this source:
              </Type>
              <div className="flex flex-wrap gap-2">
                {linkedMcpServers.map((server) => (
                  <Link
                    key={server.id}
                    to={routes.mcp.x.overview.href(mcpServerRouteParam(server))}
                    className="no-underline"
                  >
                    <Badge variant="neutral" className="hover:bg-muted">
                      <Badge.Text>{server.name || "MCP Server"}</Badge.Text>
                    </Badge>
                  </Link>
                ))}
              </div>
            </Stack>
          </Alert>
        ) : null}

        {headersQuery.isLoading || siblingsLoading ? (
          <Type muted small>
            Loading headers…
          </Type>
        ) : drafts.length === 0 ? (
          <Type muted small>
            No upstream headers configured yet.
          </Type>
        ) : (
          <Stack gap={4}>
            {suggestionsSeeded && drafts.some((draft) => !draft.id) && (
              <Type muted small>
                These headers are suggested from this endpoint's MCP catalog
                entry. Fill in the values and save, or remove the ones you don't
                need.
              </Type>
            )}
            {drafts.map((draft, index) => (
              <HeaderDraftRow
                key={draft.key}
                draft={draft}
                readOnly={readOnly}
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

        {readOnly || siblingsLoading ? null : (
          <>
            {validationError && dirty && !pristineSuggestions ? (
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
              Static secrets are redacted after save. Leave the redacted value
              unchanged to keep the current secret, or replace it to set a new
              one.
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
          </>
        )}
      </Stack>
    </div>
  );
}

function HeaderDraftRow({
  draft,
  readOnly,
  onChange,
  onRemove,
}: {
  draft: HeaderDraft;
  readOnly: boolean;
  onChange: (draft: HeaderDraft) => void;
  onRemove: () => void;
}): JSX.Element {
  const [revealed, setRevealed] = useState(false);
  // A saved secret is shown as its redacted placeholder — there's nothing to
  // reveal, so hide the toggle until the user replaces it with a new value.
  const isSavedSecret = draft.staticValue === REDACTED_SECRET;
  const showRevealToggle = draft.isSecret && !isSavedSecret;

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
              disabled={readOnly}
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
              disabled={readOnly}
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
                <SelectItem value="static">Static value</SelectItem>
                <SelectItem value="request">From request header</SelectItem>
              </SelectContent>
            </Select>
          </div>
          {readOnly ? null : (
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
          )}
        </Stack>

        {draft.source === "static" ? (
          <div>
            <Type small muted className="mb-1">
              Static value
            </Type>
            <Input
              value={draft.staticValue}
              disabled={readOnly}
              onChange={(value) => onChange({ ...draft, staticValue: value })}
              onFocus={(event) => {
                // Editing a redacted secret should replace it, not append to
                // the `***` placeholder. Select it so the first keystroke wins.
                if (draft.staticValue === REDACTED_SECRET) {
                  event.currentTarget.select();
                }
              }}
              placeholder="Bearer …"
              type={showRevealToggle && !revealed ? "password" : "text"}
              className={showRevealToggle ? "pr-10" : undefined}
            >
              {showRevealToggle ? (
                <button
                  type="button"
                  onClick={() => setRevealed((current) => !current)}
                  className="text-muted-foreground hover:text-foreground absolute top-2.5 right-3"
                  aria-label={revealed ? "Hide value" : "Show value"}
                >
                  {revealed ? (
                    <EyeOff className="size-4" />
                  ) : (
                    <Eye className="size-4" />
                  )}
                </button>
              ) : null}
            </Input>
          </div>
        ) : null}

        {draft.source === "request" ? (
          <div>
            <Type small muted className="mb-1">
              Inbound request header
            </Type>
            <Input
              value={draft.valueFromRequestHeader}
              disabled={readOnly}
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
              disabled={readOnly}
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
                disabled={readOnly}
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
