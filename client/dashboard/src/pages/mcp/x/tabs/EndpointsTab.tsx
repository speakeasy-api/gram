import { RequireScope } from "@/components/require-scope";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Type } from "@/components/ui/type";
import { useSdkClient, useSlugs } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
import { useCustomDomains } from "@/hooks/useToolsetUrl";
import { getServerURL } from "@/lib/utils";
import { useOrgRoutes } from "@/routes";
import type {
  CustomDomain,
  McpEndpoint,
  McpServer,
} from "@gram/client/models/components";
import {
  invalidateAllMcpEndpoints,
  useDeleteMcpEndpointMutation,
  useUpdateMcpEndpointMutation,
} from "@gram/client/react-query/index.js";
import { Alert, Button, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Plus, Trash2 } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { useMcpEndpointSlugValidation } from "../useMcpEndpointSlugValidation";

export function EndpointsTab({
  mcpServer,
  endpoints,
  isLoadingEndpoints,
}: {
  mcpServer: McpServer;
  endpoints: McpEndpoint[];
  isLoadingEndpoints: boolean;
}) {
  const { domains } = useCustomDomains();

  const platformEndpoint = useMemo(
    () => endpoints.find((e) => !e.customDomainId),
    [endpoints],
  );
  const customDomainEndpoints = useMemo(
    () => endpoints.filter((e) => !!e.customDomainId),
    [endpoints],
  );

  return (
    <div className="mx-auto w-full max-w-[1270px] space-y-8 px-8 py-8">
      <PlatformEndpointSurface
        mcpServer={mcpServer}
        endpoint={platformEndpoint}
        isLoading={isLoadingEndpoints}
      />
      <CustomDomainEndpointsSurface
        mcpServer={mcpServer}
        endpoints={customDomainEndpoints}
        domains={domains}
        isLoading={isLoadingEndpoints}
      />
    </div>
  );
}

function PlatformEndpointSurface({
  mcpServer,
  endpoint,
  isLoading,
}: {
  mcpServer: McpServer;
  endpoint: McpEndpoint | undefined;
  isLoading: boolean;
}) {
  const [adding, setAdding] = useState(false);

  return (
    <section>
      <Heading variant="h4" className={endpoint ? "mb-1" : "mb-4"}>
        Platform endpoint
      </Heading>
      {endpoint && (
        <Type muted small className="mb-4">
          Optional platform-hosted path. Remove to access this server only
          through custom domain paths.
        </Type>
      )}
      {isLoading ? (
        <Type muted small>
          Loading…
        </Type>
      ) : endpoint ? (
        <EndpointRow mcpServer={mcpServer} endpoint={endpoint} />
      ) : adding ? (
        <NewEndpointRow
          mcpServer={mcpServer}
          customDomainId={null}
          onClose={() => setAdding(false)}
        />
      ) : (
        <RequireScope scope="mcp:write" level="component">
          <Button variant="secondary" onClick={() => setAdding(true)}>
            <Button.LeftIcon>
              <Plus className="size-4" />
            </Button.LeftIcon>
            <Button.Text>Add platform endpoint</Button.Text>
          </Button>
        </RequireScope>
      )}
    </section>
  );
}

function CustomDomainEndpointsSurface({
  mcpServer,
  endpoints,
  domains,
  isLoading,
}: {
  mcpServer: McpServer;
  endpoints: McpEndpoint[];
  domains: Array<CustomDomain | undefined>;
  isLoading: boolean;
}) {
  const [adding, setAdding] = useState(false);
  const orgRoutes = useOrgRoutes();
  const { hasScope } = useRBAC();
  const canManageDomains = hasScope("org:admin");

  const availableDomains = domains.filter((d): d is CustomDomain => d != null);

  return (
    <section>
      <Heading variant="h4" className="mb-4">
        Custom domain endpoints
      </Heading>
      {isLoading ? (
        <Type muted small>
          Loading…
        </Type>
      ) : (
        <Stack gap={3}>
          {endpoints.map((endpoint) => (
            <EndpointRow
              key={endpoint.id}
              mcpServer={mcpServer}
              endpoint={endpoint}
              domains={availableDomains}
            />
          ))}
          {adding && (
            <NewCustomDomainEndpointRow
              mcpServer={mcpServer}
              domains={availableDomains}
              onClose={() => setAdding(false)}
            />
          )}
          {availableDomains.length === 0 ? (
            <Stack gap={2}>
              <Type muted small>
                {canManageDomains
                  ? "No custom domains configured for this organization yet."
                  : "No custom domains configured for this organization yet. Contact an organization administrator to set one up."}
              </Type>
              {canManageDomains && (
                <div>
                  <Button
                    variant="secondary"
                    onClick={() => orgRoutes.domains.goTo()}
                  >
                    <Button.LeftIcon>
                      <Plus className="size-4" />
                    </Button.LeftIcon>
                    <Button.Text>Add Custom Domain</Button.Text>
                  </Button>
                </div>
              )}
            </Stack>
          ) : (
            !adding && (
              <RequireScope scope="mcp:write" level="component">
                <Button variant="secondary" onClick={() => setAdding(true)}>
                  <Button.LeftIcon>
                    <Plus className="size-4" />
                  </Button.LeftIcon>
                  <Button.Text>Add endpoint</Button.Text>
                </Button>
              </RequireScope>
            )
          )}
        </Stack>
      )}
    </section>
  );
}

function EndpointRow({
  mcpServer,
  endpoint,
  domains,
}: {
  mcpServer: McpServer;
  endpoint: McpEndpoint;
  domains?: CustomDomain[];
}) {
  const [editing, setEditing] = useState(false);
  const [slugDraft, setSlugDraft] = useState(endpoint.slug);
  const { orgSlug } = useSlugs();
  const requiredPrefix =
    !endpoint.customDomainId && orgSlug ? `${orgSlug}-` : undefined;

  useEffect(() => {
    setSlugDraft(endpoint.slug);
  }, [endpoint.slug]);

  const queryClient = useQueryClient();
  const update = useUpdateMcpEndpointMutation({
    onSuccess: async () => {
      await invalidateAllMcpEndpoints(queryClient, { refetchType: "all" });
      toast.success("Endpoint updated");
      setEditing(false);
    },
    onError: (error) => {
      toast.error(
        error instanceof Error ? error.message : "Failed to update endpoint",
      );
    },
  });
  const remove = useDeleteMcpEndpointMutation({
    onSuccess: async () => {
      await invalidateAllMcpEndpoints(queryClient, { refetchType: "all" });
      toast.success("Endpoint removed");
    },
    onError: (error) => {
      toast.error(
        error instanceof Error ? error.message : "Failed to delete endpoint",
      );
    },
  });
  const slugError = useMcpEndpointSlugValidation(
    slugDraft.trim(),
    endpoint.customDomainId ?? null,
    endpoint.slug,
  );

  const dirty = slugDraft.trim() !== endpoint.slug;

  const customDomainLabel =
    endpoint.customDomainId &&
    domains?.find((d) => d.id === endpoint.customDomainId)?.domain;

  const handleSave = () => {
    update.mutate({
      request: {
        updateMcpEndpointForm: {
          id: endpoint.id,
          mcpServerId: mcpServer.id,
          slug: slugDraft.trim(),
          customDomainId: endpoint.customDomainId ?? undefined,
        },
      },
    });
  };

  const handleDelete = () => {
    remove.mutate({ request: { id: endpoint.id } });
  };

  const urlPrefix = customDomainLabel
    ? `https://${customDomainLabel}/x/mcp/`
    : `${getServerURL()}/x/mcp/`;

  return (
    <div className="rounded-md border p-3">
      <Stack direction="horizontal" gap={2} align="center">
        <Stack gap={0} className="min-w-0 flex-1">
          {editing ? (
            <Stack direction="horizontal" align="center">
              <Type muted mono variant="small">
                {urlPrefix}
              </Type>
              <Input
                value={slugDraft}
                onChange={(value) => setSlugDraft(value)}
                requiredPrefix={requiredPrefix}
              />
            </Stack>
          ) : (
            <Stack direction="horizontal" align="center" gap={0}>
              <Type muted mono variant="small">
                {urlPrefix}
              </Type>
              <Type small className="truncate font-mono">
                {endpoint.slug}
              </Type>
            </Stack>
          )}
        </Stack>
        <RequireScope scope="mcp:write" level="component">
          {editing ? (
            <>
              <Button
                size="md"
                variant="primary"
                disabled={!dirty || !!slugError || update.isPending}
                onClick={handleSave}
              >
                <Button.Text>Save</Button.Text>
              </Button>
              <Button
                size="md"
                variant="secondary"
                disabled={update.isPending}
                onClick={() => {
                  setSlugDraft(endpoint.slug);
                  setEditing(false);
                }}
              >
                <Button.Text>Cancel</Button.Text>
              </Button>
            </>
          ) : (
            <>
              <Button
                size="md"
                variant="secondary"
                onClick={() => setEditing(true)}
              >
                <Button.Text>Edit</Button.Text>
              </Button>
              <Button
                size="md"
                variant="destructive-secondary"
                disabled={remove.isPending}
                onClick={handleDelete}
              >
                <Button.LeftIcon>
                  <Trash2 className="size-4" />
                </Button.LeftIcon>
                <Button.Text>Delete</Button.Text>
              </Button>
            </>
          )}
        </RequireScope>
      </Stack>
      {editing && slugError && (
        <Alert variant="error" dismissible={false} className="mt-2">
          {slugError}
        </Alert>
      )}
      {update.isError && (
        <Alert variant="error" dismissible={false} className="mt-2">
          {update.error.message}
        </Alert>
      )}
    </div>
  );
}

function NewEndpointRow({
  mcpServer,
  customDomainId,
  onClose,
}: {
  mcpServer: McpServer;
  customDomainId: string | null;
  onClose: () => void;
}) {
  const [slug, setSlug] = useState("");
  const queryClient = useQueryClient();
  const client = useSdkClient();
  const { orgSlug } = useSlugs();
  const requiredPrefix = !customDomainId && orgSlug ? `${orgSlug}-` : undefined;
  const slugError = useMcpEndpointSlugValidation(slug.trim(), customDomainId);

  const [submitting, setSubmitting] = useState(false);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  const handleCreate = async () => {
    const trimmed = slug.trim();
    if (!trimmed || slugError) return;
    setSubmitting(true);
    setErrorMsg(null);
    try {
      await client.mcpEndpoints.create({
        createMcpEndpointForm: {
          mcpServerId: mcpServer.id,
          slug: trimmed,
          customDomainId: customDomainId ?? undefined,
        },
      });
      await invalidateAllMcpEndpoints(queryClient, { refetchType: "all" });
      toast.success("Endpoint added");
      onClose();
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "Failed to add endpoint";
      setErrorMsg(message);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="rounded-md border p-3">
      <Stack direction="horizontal" gap={2} align="center">
        <Stack direction="horizontal" align="center" className="min-w-0 flex-1">
          {!customDomainId && (
            <Type muted mono variant="small">
              {`${getServerURL()}/x/mcp/`}
            </Type>
          )}
          <Input
            value={slug}
            onChange={(value) => setSlug(value)}
            placeholder="my-endpoint"
            requiredPrefix={requiredPrefix}
          />
        </Stack>
        <Button
          size="md"
          variant="primary"
          disabled={!slug.trim() || !!slugError || submitting}
          onClick={handleCreate}
        >
          <Button.Text>Add</Button.Text>
        </Button>
        <Button
          size="md"
          variant="secondary"
          disabled={submitting}
          onClick={onClose}
        >
          <Button.Text>Cancel</Button.Text>
        </Button>
      </Stack>
      {slug.trim() && slugError && (
        <Alert variant="error" dismissible={false} className="mt-2">
          {slugError}
        </Alert>
      )}
      {errorMsg && (
        <Alert variant="error" dismissible={false} className="mt-2">
          {errorMsg}
        </Alert>
      )}
    </div>
  );
}

function NewCustomDomainEndpointRow({
  mcpServer,
  domains,
  onClose,
}: {
  mcpServer: McpServer;
  domains: CustomDomain[];
  onClose: () => void;
}) {
  const [domainId, setDomainId] = useState<string>(domains[0]?.id ?? "");
  const [slug, setSlug] = useState("");
  const queryClient = useQueryClient();
  const client = useSdkClient();
  const slugError = useMcpEndpointSlugValidation(slug.trim(), domainId || null);

  const [submitting, setSubmitting] = useState(false);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  const handleCreate = async () => {
    const trimmed = slug.trim();
    if (!trimmed || !domainId || slugError) return;
    setSubmitting(true);
    setErrorMsg(null);
    try {
      await client.mcpEndpoints.create({
        createMcpEndpointForm: {
          mcpServerId: mcpServer.id,
          slug: trimmed,
          customDomainId: domainId,
        },
      });
      await invalidateAllMcpEndpoints(queryClient, { refetchType: "all" });
      toast.success("Endpoint added");
      onClose();
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "Failed to add endpoint";
      setErrorMsg(message);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="rounded-md border p-3">
      <Stack direction="horizontal" gap={2} align="center">
        <Stack
          direction="horizontal"
          align="center"
          gap={1}
          className="min-w-0 flex-1"
        >
          <Type muted mono variant="small">
            https://
          </Type>
          <Select
            value={domainId}
            onValueChange={(value) => setDomainId(value)}
          >
            <SelectTrigger>
              <SelectValue placeholder="Custom domain" />
            </SelectTrigger>
            <SelectContent>
              {domains.map((domain) => (
                <SelectItem key={domain.id} value={domain.id}>
                  {domain.domain}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Type muted mono variant="small">
            /x/mcp/
          </Type>
          <Input
            value={slug}
            onChange={(value) => setSlug(value)}
            placeholder="my-endpoint"
          />
        </Stack>
        <Button
          size="md"
          variant="primary"
          disabled={!slug.trim() || !domainId || !!slugError || submitting}
          onClick={handleCreate}
        >
          <Button.Text>Add</Button.Text>
        </Button>
        <Button
          size="md"
          variant="secondary"
          disabled={submitting}
          onClick={onClose}
        >
          <Button.Text>Cancel</Button.Text>
        </Button>
      </Stack>
      {slug.trim() && slugError && (
        <Alert variant="error" dismissible={false} className="mt-2">
          {slugError}
        </Alert>
      )}
      {errorMsg && (
        <Alert variant="error" dismissible={false} className="mt-2">
          {errorMsg}
        </Alert>
      )}
    </div>
  );
}
