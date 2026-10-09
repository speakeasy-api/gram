import { InlineEmptyState } from "@/components/inline-empty-state";
import { FormPage } from "@/components/page-templates";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Field, FieldError, FieldLabel } from "@/components/ui/Field";
import { Input } from "@/components/ui/Input";
import { Skeleton } from "@/components/ui/Skeleton";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { UserSessionIssuerSelect } from "@/components/user-session-issuer-select";
import {
  PROJECT_SPECIFIC_ISSUER_VALUE,
  defaultCreationUserSessionIssuerValue,
} from "@/components/user-session-issuer-select.utils";
import { useEffectiveUserSessionIssuers } from "@/hooks/useEffectiveUserSessionIssuers";
import { mcpServerRouteParam } from "@/lib/sources";
import { GatewayAttachmentStatus } from "@/pages/mcp/gateway/GatewayAttachmentStatus";
import { useGatewayCreation } from "@/pages/mcp/gateway/useGatewayCreation";
import { NewServerGuardrailSection } from "@/pages/security/server-guardrails/NewServerGuardrailSection";
import {
  guardrailFailureMessage,
  useNewServerGuardrail,
  type NewServerGuardrailOutcome,
} from "@/pages/security/server-guardrails/useNewServerGuardrail";
import { useRoutes } from "@/routes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { TunneledMcpServer } from "@gram/client/models/components/tunneledmcpserver.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import { useMetaMcpMembers } from "@gram/client/react-query/metaMcpMembers.js";
import { useTunneledMcpServers } from "@gram/client/react-query/tunneledMcpServers.js";
import { Loader2 } from "lucide-react";
import { useMemo, useState, type ReactNode } from "react";
import { Link } from "react-router";
import { toast } from "sonner";
import { DeleteUnusedTunnelDialog } from "./DeleteUnusedTunnelDialog";
import { ExistingTunnelCreated } from "./ExistingTunnelCreated";
import {
  ExistingTunnelPicker,
  type ExistingTunnelOption,
} from "./ExistingTunnelPicker";
import { isDefiniteRejection } from "./existingTunnel";
import {
  useCreateMcpServerOnExistingTunnel,
  type CreateMcpServerOnExistingTunnelData,
} from "./hooks";

const GATEWAY_ALREADY_ON_TUNNEL =
  "This gateway already includes an MCP server on this tunnel.";

// Builds the picker rows: each tunnel with the MCP servers the caller can see
// on it, and whether the target gateway already holds one of them. Hidden
// members are not known here; the server stays the authority on attaching.
function tunnelOptions(
  tunnels: TunneledMcpServer[],
  servers: McpServer[],
  gatewayMemberIds: ReadonlySet<string>,
): ExistingTunnelOption[] {
  return tunnels.map((tunnel) => {
    const onTunnel = servers.filter(
      (server) => server.tunneledMcpServerId === tunnel.id,
    );
    const inGateway = onTunnel.some((server) =>
      gatewayMemberIds.has(server.id),
    );
    return {
      tunnel,
      servers: onTunnel,
      unavailableReason: inGateway ? GATEWAY_ALREADY_ON_TUNNEL : undefined,
    };
  });
}

/**
 * Adds an MCP server to a tunnel that already exists in the project. Nothing
 * here creates, rotates, or (on failure) deletes the tunnel.
 */
export function ExistingTunnelFlow({
  modeSwitch,
  onNewTunnel,
  requestedTunnelId,
}: {
  modeSwitch: (disabled: boolean) => ReactNode;
  /** Switches the page to creating a new tunnel. */
  onNewTunnel: () => void;
  /** A tunnel named in the URL, e.g. from "Add MCP server on this tunnel". */
  requestedTunnelId: string | null;
}): JSX.Element {
  const routes = useRoutes();
  const flow = useGatewayCreation();
  const create = useCreateMcpServerOnExistingTunnel();
  const issuerQuery = useEffectiveUserSessionIssuers();
  const newGuardrail = useNewServerGuardrail();
  const tunnelsQuery = useTunneledMcpServers(undefined, undefined, {
    throwOnError: false,
  });
  const serversQuery = useMcpServers(undefined, undefined, {
    throwOnError: false,
  });
  const membersQuery = useMetaMcpMembers(
    { metaMcpServerId: flow.gatewayId ?? "" },
    undefined,
    { enabled: !!flow.gatewayId, throwOnError: false },
  );

  const [selection, setSelection] = useState<string | null>(requestedTunnelId);
  const [name, setName] = useState("");
  const [touched, setTouched] = useState(false);
  const [issuerSelection, setIssuerSelection] = useState<string | null>(null);
  // A create whose outcome is unknown: it may have committed before the
  // response was lost, so the user decides whether to try again.
  const [uncertain, setUncertain] = useState(false);
  const [created, setCreated] = useState<
    (CreateMcpServerOnExistingTunnelData & { tunnel: TunneledMcpServer }) | null
  >(null);
  const [guardrailOutcome, setGuardrailOutcome] =
    useState<NewServerGuardrailOutcome | null>(null);
  const [deleteTunnel, setDeleteTunnel] = useState<TunneledMcpServer | null>(
    null,
  );

  const tunnels = useMemo(
    () => tunnelsQuery.data?.tunneledMcpServers ?? [],
    [tunnelsQuery.data],
  );
  const memberIds = useMemo(
    () =>
      new Set(
        (membersQuery.data?.members ?? []).map((member) => member.mcpServerId),
      ),
    [membersQuery.data],
  );
  const options = tunnelOptions(
    tunnels,
    serversQuery.data?.mcpServers ?? [],
    memberIds,
  );
  const selected = options.find(
    (option) =>
      option.tunnel.id === selection && option.unavailableReason === undefined,
  );
  const requestedMissing =
    requestedTunnelId !== null &&
    tunnelsQuery.isSuccess &&
    !tunnels.some((tunnel) => tunnel.id === requestedTunnelId);

  const selectedIssuer =
    issuerSelection ??
    defaultCreationUserSessionIssuerValue(issuerQuery.organizationIssuers);
  const nameError = name.trim() ? null : "Display name is required";
  const locked = create.isPending || flow.isAttaching;
  // The gateway's members decide which tunnels it can still take; until they
  // are read, a tunnel it already fronts would look available.
  const membersUnknown = !!flow.gatewayId && !membersQuery.isSuccess;
  const submitDisabled =
    locked ||
    membersUnknown ||
    !selected ||
    nameError !== null ||
    !newGuardrail.validation.ok ||
    issuerQuery.isLoading ||
    issuerQuery.isError ||
    selectedIssuer === "";

  const select = (id: string) => {
    setSelection(id);
    setUncertain(false);
    create.reset();
  };

  const submit = async () => {
    setTouched(true);
    if (submitDisabled || !selected) return;
    setUncertain(false);
    try {
      const result = await create.mutateAsync({
        tunneledMcpServerId: selected.tunnel.id,
        name: name.trim(),
        userSessionIssuerId:
          selectedIssuer === PROJECT_SPECIFIC_ISSUER_VALUE
            ? undefined
            : selectedIssuer,
      });
      setCreated({ ...result, tunnel: selected.tunnel });
      toast.success("MCP server added to tunnel");
      const outcome = await newGuardrail.createFor(result.mcpServer);
      setGuardrailOutcome(outcome);
      if (outcome.status === "failed") {
        toast.error(guardrailFailureMessage(outcome), { duration: 12000 });
      }
    } catch (error) {
      if (!isDefiniteRejection(error)) setUncertain(true);
    }
  };

  if (created) {
    return (
      <ExistingTunnelCreated
        mcpServer={created.mcpServer}
        tunnel={created.tunnel}
        endpointCreated={created.endpointCreated}
        flow={flow}
        guardrailOutcome={guardrailOutcome}
      />
    );
  }

  return (
    <FormPage
      scope="mcp:write"
      title="New tunneled MCP server"
      description="Add an MCP server to a tunnel that is already set up. It uses the tunnel's existing key and agent."
    >
      <form
        onSubmit={(event) => {
          event.preventDefault();
          void submit();
        }}
        noValidate
      >
        <Stack gap={4}>
          {modeSwitch(locked)}
          {requestedMissing ? (
            <Alert variant="error" dismissible={false}>
              The requested tunnel is not in this project or was deleted. Choose
              a tunnel below.
            </Alert>
          ) : null}
          <Stack gap={1}>
            <Text small className="font-medium">
              Tunnel
            </Text>
            <TunnelChoice
              isLoading={
                tunnelsQuery.isPending ||
                serversQuery.isPending ||
                (!!flow.gatewayId && membersQuery.isPending)
              }
              isError={
                tunnelsQuery.isError ||
                serversQuery.isError ||
                membersQuery.isError
              }
              onRetry={() => {
                void tunnelsQuery.refetch();
                void serversQuery.refetch();
                if (flow.gatewayId) void membersQuery.refetch();
              }}
            >
              {options.length === 0 ? (
                <InlineEmptyState
                  icon="cable"
                  heading="No tunnels in this project"
                  description="Create a tunnel first; this page then adds more MCP servers to it."
                  action={
                    <Button
                      type="button"
                      variant="secondary"
                      onClick={onNewTunnel}
                    >
                      <Button.Text>New tunnel</Button.Text>
                    </Button>
                  }
                />
              ) : null}
              <ExistingTunnelPicker
                options={options}
                selectedId={selected?.tunnel.id ?? null}
                disabled={locked}
                onSelect={select}
                onDeleteUnused={setDeleteTunnel}
                serverHref={(server) =>
                  routes.mcp.x.settings.href(mcpServerRouteParam(server))
                }
              />
            </TunnelChoice>
            {selected?.tunnel.resourceIdentifier ? (
              <Text muted small>
                Resource identifier (shared by the tunnel):{" "}
                <span className="font-mono">
                  {selected.tunnel.resourceIdentifier}
                </span>
              </Text>
            ) : null}
          </Stack>

          <Field data-invalid={touched && nameError ? true : undefined}>
            <FieldLabel htmlFor="existing-tunnel-server-name">
              Display name
            </FieldLabel>
            <Input
              id="existing-tunnel-server-name"
              placeholder="Internal MCP server (sandbox)"
              value={name}
              disabled={locked}
              onChange={setName}
              onBlur={() => setTouched(true)}
              aria-invalid={touched && nameError ? true : undefined}
            />
            {touched && nameError ? <FieldError>{nameError}</FieldError> : null}
          </Field>

          <Stack gap={1}>
            <Text small className="font-medium">
              User session issuer
            </Text>
            <UserSessionIssuerSelect
              issuers={issuerQuery.organizationIssuers}
              value={selectedIssuer}
              onValueChange={setIssuerSelection}
              includeProjectSpecific
              disabled={locked || issuerQuery.isLoading || issuerQuery.isError}
            />
          </Stack>

          <NewServerGuardrailSection
            guardrail={newGuardrail}
            serverName={name.trim() || "this server"}
            disabled={locked}
          />
          <GatewayAttachmentStatus flow={flow} />

          {create.isError && !uncertain ? (
            <Alert variant="error" dismissible={false}>
              {create.error.message}
            </Alert>
          ) : null}
          {uncertain && selected ? (
            <UncertainCreateAlert
              servers={selected.servers}
              serverHref={(server) =>
                routes.mcp.x.settings.href(mcpServerRouteParam(server))
              }
            />
          ) : null}

          <Stack direction="horizontal" gap={2}>
            <Button type="submit" variant="primary" disabled={submitDisabled}>
              {create.isPending ? (
                <Button.LeftIcon>
                  <Loader2 className="size-4 animate-spin" />
                </Button.LeftIcon>
              ) : null}
              <Button.Text>
                {submitLabel(create.isPending, uncertain)}
              </Button.Text>
            </Button>
            <Button
              type="button"
              variant="secondary"
              disabled={locked}
              onClick={() => {
                if (!flow.cancel()) routes.mcp.add.goTo();
              }}
            >
              <Button.Text>Cancel</Button.Text>
            </Button>
          </Stack>
        </Stack>
      </form>
      <DeleteUnusedTunnelDialog
        tunnel={deleteTunnel}
        onClose={() => setDeleteTunnel(null)}
      />
    </FormPage>
  );
}

function submitLabel(pending: boolean, uncertain: boolean): string {
  if (pending) return "Adding";
  if (uncertain) return "Create anyway";
  return "Add server";
}

function TunnelChoice({
  isLoading,
  isError,
  onRetry,
  children,
}: {
  isLoading: boolean;
  isError: boolean;
  onRetry: () => void;
  children: ReactNode;
}): JSX.Element {
  if (isError) {
    return (
      <Alert variant="error" dismissible={false}>
        <Stack gap={2}>
          <Text small>Could not load this project&apos;s tunnels.</Text>
          <div>
            <Button
              type="button"
              variant="secondary"
              size="sm"
              onClick={onRetry}
            >
              <Button.Text>Retry</Button.Text>
            </Button>
          </div>
        </Stack>
      </Alert>
    );
  }
  if (isLoading) return <Skeleton className="h-24 w-full" />;
  return <>{children}</>;
}

// The create request failed in a way that does not say whether it committed.
// Retrying blindly could add a duplicate server, so the tunnel's current
// servers are shown and retrying is an explicit choice.
function UncertainCreateAlert({
  servers,
  serverHref,
}: {
  servers: McpServer[];
  serverHref: (server: McpServer) => string;
}): JSX.Element {
  return (
    <Alert variant="warning" dismissible={false}>
      <Stack gap={2}>
        <Text small>
          The request may have succeeded before the connection was lost. Check
          the MCP servers on this tunnel before trying again; Create anyway may
          add a duplicate.
        </Text>
        {servers.length > 0 ? (
          <ul className="list-disc pl-6">
            {servers.map((server) => (
              <li key={server.id}>
                <Link
                  to={serverHref(server)}
                  className="underline underline-offset-2"
                >
                  {server.name || "MCP Server"}
                </Link>
              </li>
            ))}
          </ul>
        ) : (
          <Text small muted>
            No MCP servers you can view use this tunnel yet.
          </Text>
        )}
      </Stack>
    </Alert>
  );
}
