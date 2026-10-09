import {
  FooterSaveButton,
  SettingsSection,
} from "@/components/detail/settings-section";
import { RequireScope } from "@/components/require-scope";
import { Alert } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Field, FieldLabel } from "@/components/ui/Field";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { type Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { useSdkClient } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
import { invalidateUpstreamHeaderDependents } from "@/lib/environment-header-dependents";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { McpServerEnvironmentHeader } from "@gram/client/models/components/mcpserverenvironmentheader.js";
import type { GetMcpServerEnvironmentHeadersRequest } from "@gram/client/models/operations/getmcpserverenvironmentheaders.js";
import {
  buildGetMcpServerQuery,
  invalidateAllGetMcpServer,
} from "@gram/client/react-query/getMcpServer.js";
import { useGetMcpServerEnvironmentHeaders } from "@gram/client/react-query/getMcpServerEnvironmentHeaders.js";
import { invalidateAllMcpServers } from "@gram/client/react-query/mcpServers.js";
import { useUpdateMcpServerMutation } from "@gram/client/react-query/updateMcpServer.js";
import { useQueryClient } from "@tanstack/react-query";
import { type ComponentProps, useState } from "react";
import { toast } from "sonner";

const MCP_ENVIRONMENT_HEADERS_SECTION_ID = "environment-headers";

// Radix Select disallows an empty-string value, so "no environment" needs a
// sentinel that maps back to an unlinked server when saved.
const NO_ENVIRONMENT = "__none__";

type BadgeVariant = ComponentProps<typeof Badge>["variant"];

const STATUS_LABELS: Record<
  McpServerEnvironmentHeader["status"],
  { label: string; variant: BadgeVariant }
> = {
  mapped: { label: "Mapped", variant: "success" },
  overrides_source: {
    label: "Overrides source header",
    variant: "information",
  },
  invalid_name: { label: "Invalid name", variant: "destructive" },
  reserved: { label: "Reserved header", variant: "destructive" },
  empty_value: { label: "Empty value", variant: "destructive" },
  invalid_value: { label: "Invalid value", variant: "destructive" },
  duplicate: { label: "Duplicate header", variant: "destructive" },
  undecryptable: { label: "Cannot decrypt", variant: "destructive" },
  not_mapped: { label: "Not used", variant: "neutral" },
};

const columns: Column<McpServerEnvironmentHeader>[] = [
  {
    key: "entry",
    header: "Environment entry",
    render: (row) => <Text mono>{row.entryName}</Text>,
  },
  {
    key: "header",
    header: "Header",
    render: (row) => <Text mono>{row.headerName ?? "—"}</Text>,
  },
  {
    key: "status",
    header: "Status",
    width: "220px",
    render: (row) => (
      <Badge variant={STATUS_LABELS[row.status].variant}>
        {STATUS_LABELS[row.status].label}
      </Badge>
    ),
  },
];

/**
 * Picks the preview the API should compute for the drafted environment. The
 * environment id is always part of the request, and so of its cache key, so a
 * result for an earlier pick, or for the link before it changed elsewhere,
 * never renders for the current one.
 */
function previewRequest(
  mcpServerId: string,
  draft: string,
): GetMcpServerEnvironmentHeadersRequest {
  if (draft === NO_ENVIRONMENT) return { id: mcpServerId, selection: "none" };
  return { id: mcpServerId, selection: "environment", environmentId: draft };
}

function isForbidden(error: unknown): boolean {
  return error instanceof GramError && error.statusCode === 403;
}

/**
 * Links this MCP server to an environment whose MCP_HEADER_<Header-Name>
 * entries it sends upstream. The link belongs to this server alone, unlike
 * the source's headers, and changing it needs project-wide environment read
 * access in addition to write access to the server, as the API requires.
 */
export function EnvironmentHeadersSection({
  mcpServer,
}: {
  mcpServer: McpServer;
}): JSX.Element {
  const projectId = mcpServer.projectId;
  const { hasScope, isLoading: rbacLoading } = useRBAC();
  const canReadEnvironments =
    !rbacLoading && hasScope("environment:read", projectId, projectId);
  const canWrite =
    !rbacLoading && hasScope("mcp:write", mcpServer.id, projectId);

  return (
    <SettingsSection id={MCP_ENVIRONMENT_HEADERS_SECTION_ID}>
      <SettingsSection.Header>
        <SettingsSection.Title>Environment Headers</SettingsSection.Title>
        <SettingsSection.Description>
          Send headers that differ per MCP server, such as an instance URL or
          tenant, from a linked environment. Only entries named{" "}
          <code>MCP_HEADER_&lt;Header-Name&gt;</code> are sent; every other
          variable in the environment is ignored. Underscores in the name become
          dashes, so <code>MCP_HEADER_X_INSTANCE_URL</code> is sent as{" "}
          <code>X-Instance-Url</code>. An environment header replaces a source
          header with the same name and applies to this MCP server only.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        {canReadEnvironments ? (
          <EnvironmentHeadersEditor mcpServer={mcpServer} canWrite={canWrite} />
        ) : (
          <SettingsSection.Body>
            <Text muted small>
              {rbacLoading
                ? "Checking access…"
                : "Viewing or changing environment headers requires read access to every environment in this project."}
            </Text>
          </SettingsSection.Body>
        )}
      </SettingsSection.Panel>
    </SettingsSection>
  );
}

function EnvironmentHeadersEditor({
  mcpServer,
  canWrite,
}: {
  mcpServer: McpServer;
  canWrite: boolean;
}): JSX.Element {
  const queryClient = useQueryClient();
  const client = useSdkClient();
  const linked = mcpServer.environmentId ?? NO_ENVIRONMENT;
  // The draft remembers the link it was started from. When the stored link
  // changes (a save landing, another tab, another operator), a draft that is
  // untouched or already equals the new link follows it; a divergent edit is
  // kept and flagged so Save never silently restores a link the user did not
  // choose.
  const [editing, setEditing] = useState({ base: linked, draft: linked });
  if (
    editing.base !== linked &&
    (editing.draft === editing.base || editing.draft === linked)
  ) {
    setEditing({ base: linked, draft: linked });
  }
  const draft = editing.draft;
  const linkChangedElsewhere = editing.base !== linked;
  const setDraft = (value: string) =>
    setEditing({ base: linked, draft: value });

  const options = useGetMcpServerEnvironmentHeaders(
    { id: mcpServer.id, selection: "none" },
    undefined,
    { throwOnError: false },
  );
  const preview = useGetMcpServerEnvironmentHeaders(
    previewRequest(mcpServer.id, draft),
    undefined,
    { throwOnError: false },
  );

  const update = useUpdateMcpServerMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllGetMcpServer(queryClient, { refetchType: "all" }),
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
        // The upstream's tool list can differ per environment, so a listing
        // cached under the old link must not be shown for the new one. The
        // query client's mutation hook does this too; awaiting it here keeps
        // the success toast after the refresh.
        invalidateUpstreamHeaderDependents(queryClient),
      ]);
      toast.success("Environment link saved");
    },
    onError: (error) =>
      toast.error(
        error instanceof Error ? error.message : "Failed to save environment",
      ),
  });

  // mcpServers.update is a full-record replace, so the form is built from the
  // latest stored server rather than this page's copy: a visibility or other
  // change saved elsewhere since the page loaded is kept, and a link changed
  // elsewhere stops the save so the user can review it first.
  const [isFetchingLatest, setIsFetchingLatest] = useState(false);
  const save = async () => {
    setIsFetchingLatest(true);
    try {
      const latest = await queryClient.fetchQuery({
        ...buildGetMcpServerQuery(client, { id: mcpServer.id }),
        staleTime: 0,
      });
      if ((latest.environmentId ?? NO_ENVIRONMENT) !== editing.base) {
        await invalidateAllGetMcpServer(queryClient, { refetchType: "all" });
        toast.error(
          "This server's environment was changed elsewhere. Review it before saving.",
        );
        return;
      }
      update.mutate({
        request: {
          updateMcpServerForm: {
            id: latest.id,
            name: latest.name ?? undefined,
            remoteMcpServerId: latest.remoteMcpServerId ?? undefined,
            tunneledMcpServerId: latest.tunneledMcpServerId ?? undefined,
            toolsetId: latest.toolsetId ?? undefined,
            unproxiedMcpServerId: latest.unproxiedMcpServerId ?? undefined,
            toolVariationsGroupId: latest.toolVariationsGroupId ?? undefined,
            environmentId: draft === NO_ENVIRONMENT ? undefined : draft,
            visibility: latest.visibility,
          },
        },
      });
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : "Failed to save environment",
      );
    } finally {
      setIsFetchingLatest(false);
    }
  };
  const saving = isFetchingLatest || update.isPending;

  const environments = options.data?.environments ?? [];
  const linkedMissing =
    linked !== NO_ENVIRONMENT && !environments.some((e) => e.id === linked);
  const result = preview.data;
  const dirty = draft !== linked;
  const loadFailed = options.isError || preview.isError;

  return (
    <>
      <SettingsSection.Body>
        {loadFailed ? (
          <Alert variant="error" dismissible={false}>
            {isForbidden(preview.error) || isForbidden(options.error)
              ? "You don't have read access to this environment, so its headers can't be previewed or linked. Saving is disabled."
              : "Could not load this server's environment headers. Saving is disabled."}
          </Alert>
        ) : null}
        <Field>
          <FieldLabel htmlFor="mcp-server-environment">Environment</FieldLabel>
          <RequireScope
            scope="mcp:write"
            resourceId={mcpServer.id}
            projectId={mcpServer.projectId}
            level="component"
          >
            <Select
              value={draft}
              disabled={!canWrite || saving || !options.data}
              onValueChange={setDraft}
            >
              <SelectTrigger id="mcp-server-environment" className="w-72">
                <SelectValue placeholder="Loading environments" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={NO_ENVIRONMENT}>None</SelectItem>
                {linkedMissing ? (
                  <SelectItem value={linked}>
                    Unavailable environment
                  </SelectItem>
                ) : null}
                {environments.map((env) => (
                  <SelectItem key={env.id} value={env.id}>
                    {env.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </RequireScope>
        </Field>
        {linkChangedElsewhere ? (
          <Alert variant="warning" dismissible={false}>
            This server's environment was changed elsewhere while you were
            editing. Review your selection before saving.
          </Alert>
        ) : null}
        {result ? (
          <EnvironmentHeadersPreview result={result} isCandidate={dirty} />
        ) : null}
        <Text muted small>
          A signed-in user's upstream token replaces an environment
          Authorization header. Environments do not give MCP servers on one
          tunnel separate tunnel identities, audiences or public quotas, a
          public server sends these headers for anonymous callers, and a gateway
          cannot include two MCP servers on the same tunnel.
        </Text>
      </SettingsSection.Body>
      {dirty ? (
        <SettingsSection.Footer>
          <SettingsSection.FooterActions>
            <RequireScope
              scope="mcp:write"
              resourceId={mcpServer.id}
              projectId={mcpServer.projectId}
              level="component"
            >
              <FooterSaveButton
                pending={saving}
                disabled={!canWrite || saving || loadFailed || !result}
                onClick={() => void save()}
              />
            </RequireScope>
          </SettingsSection.FooterActions>
        </SettingsSection.Footer>
      ) : null}
    </>
  );
}

/**
 * The preview for the linked environment describes what the server does now;
 * the preview for an unsaved selection describes what saving it would do.
 */
function EnvironmentHeadersPreview({
  result,
  isCandidate,
}: {
  result: NonNullable<
    ReturnType<typeof useGetMcpServerEnvironmentHeaders>["data"]
  >;
  isCandidate: boolean;
}): JSX.Element | null {
  if (result.environmentStatus === "none") return null;
  if (result.environmentStatus === "unavailable") {
    return (
      <Alert variant="error" dismissible={false}>
        {isCandidate
          ? "This environment is deleted or unavailable, so it cannot be linked."
          : "The linked environment is deleted or unavailable, so requests to this server are refused. Choose another environment or None and save."}
      </Alert>
    );
  }
  return (
    <>
      {result.environmentConfigurationInvalid ? (
        <Alert variant="error" dismissible={false}>
          {isCandidate
            ? "Requests to this server would be refused if you save this environment, because an MCP_HEADER_ entry cannot be sent. Fix or remove the marked entries first, or choose another environment."
            : "Requests to this server are refused while an MCP_HEADER_ entry cannot be sent. Fix or remove the entries marked below in the environment."}
        </Alert>
      ) : null}
      <Table
        columns={columns}
        data={result.entries}
        rowKey={(row) => row.entryName}
        noResultsMessage={
          <Text muted small>
            This environment has no MCP_HEADER_ entries, so it adds no headers.
          </Text>
        }
      />
    </>
  );
}
