import { invalidateAllMetaMcpMembers } from "@gram/client/react-query/metaMcpMembers";
import { toast } from "sonner";
import {
  buildServerGuardrailRequest,
  effectiveAction,
} from "@/pages/security/server-guardrails/server-guardrail-policy";
import { policyNewPrefillQuery } from "@/pages/security/policy-new-prefill";
import { useDetectorMode } from "@/pages/security/use-detector-mode";
import { invalidateAllRiskListPolicies } from "@gram/client/react-query/riskListPolicies";
import {
  useCallback,
  useEffect,
  useReducer,
  useRef,
  useState,
  type Dispatch,
} from "react";
import {
  Navigate,
  useNavigate,
  useParams,
  useSearchParams,
} from "react-router";
import { useOrganization, useProject, useSession } from "@/contexts/Auth";
import { useSdkClient, useSlugs } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import { RequireScope } from "@/components/require-scope";
import { Alert } from "@/components/ui/Alert";
import { Skeleton } from "@/components/ui/Skeleton";
import { Button } from "@/components/ui/Button";
import { useProviderSetupRollout } from "@/hooks/useProviderSetupRollout";
import { useListMCPCatalog } from "./hooks";
import {
  readSnowflakeHandoff,
  type SnowflakeHandoff,
  isSnowflakeCatalogServer,
} from "./snowflake-install";
import { SetupContext } from "./provider-setup/setup-context";
import {
  reduce,
  validate,
  type State,
  type Action,
} from "./provider-setup/state";
import {
  snowflakeSelectionForUrl,
  snowflakeConnection,
} from "./provider-setup/snowflake";
import { SetupBody } from "./provider-setup/SetupBody";
import { useProviderConnection } from "@/pages/mcp/x/setup/useProviderConnection";
import {
  loadProviderProgress,
  saveProviderProgress,
  providerProgressKey,
  providerSourceFingerprint,
} from "@/pages/mcp/x/setup/provider-setup";
import { mcpServerRouteParam } from "@/lib/sources";
import { createDefaultMcpEndpoint } from "@/lib/mcpEndpoints";
import { useQueryClient } from "@tanstack/react-query";
import { useGetMcpServer } from "@gram/client/react-query/getMcpServer";
import { useGetRemoteMcpServer } from "@gram/client/react-query/getRemoteMcpServer";
import { invalidateAllMcpServers } from "@gram/client/react-query/mcpServers";
import { invalidateAllRemoteMcpServers } from "@gram/client/react-query/remoteMcpServers";
import type { McpServer } from "@gram/client/models/components/mcpserver";

export default function SnowflakeCatalogSetup(): JSX.Element {
  const routes = useRoutes();
  const { serverSpecifier = "" } = useParams();
  const catalog = useListMCPCatalog();
  const rollout = useProviderSetupRollout();
  if (rollout === "loading" || catalog.isLoading)
    return <Skeleton className="h-64 w-full" />;
  if (catalog.isError)
    return (
      <Alert variant="error">
        Couldn’t load the catalog.{" "}
        <Button variant="secondary" onClick={() => void catalog.refetch()}>
          <Button.Text>Retry</Button.Text>
        </Button>
      </Alert>
    );
  const entry = catalog.data?.servers.find(
    (server) =>
      server.registrySpecifier === decodeURIComponent(serverSpecifier),
  );
  if (rollout !== "enabled" || !entry || !isSnowflakeCatalogServer(entry))
    return <Navigate to={routes.mcp.catalog.href()} replace />;
  return (
    <RequireScope
      scope={["project:read", "mcp:write", "project:write"]}
      level="page"
    >
      <SnowflakeCatalogFlow />
    </RequireScope>
  );
}

// Draft setup uses the approved tasks before any account-specific source exists.
// Only the credentials action creates records; OAuth and verification stay shared.
export function SnowflakeCatalogFlow(): JSX.Element {
  const org = useOrganization(),
    project = useProject(),
    session = useSession();
  const key = providerProgressKey(
    "snowflake",
    session.user.id,
    org.id,
    project.id,
    "catalog:com.snowflake/cortex-mcp",
  );
  const routes = useRoutes();
  const handoff = readSnowflakeHandoff(key);
  if (!handoff)
    return (
      <Navigate
        to={routes.mcp.catalog.detail.href(
          encodeURIComponent("com.snowflake/cortex-mcp"),
        )}
        replace
      />
    );
  return <CatalogDraft key={key} storageKey={key} handoff={handoff} />;
}
function readCheckpoint(key: string): {
  remoteId?: string;
  serverId?: string;
  sourceFingerprint?: string;
  guardrailAttempted?: boolean;
} {
  try {
    const value: unknown = JSON.parse(sessionStorage.getItem(key) ?? "null");
    if (!value || typeof value !== "object") return {};
    return {
      guardrailAttempted:
        "guardrailAttempted" in value && value.guardrailAttempted === true,
      sourceFingerprint:
        "sourceFingerprint" in value &&
        typeof value.sourceFingerprint === "string"
          ? value.sourceFingerprint
          : undefined,
      remoteId:
        "remoteId" in value && typeof value.remoteId === "string"
          ? value.remoteId
          : undefined,
      serverId:
        "serverId" in value && typeof value.serverId === "string"
          ? value.serverId
          : undefined,
    };
  } catch {
    return {};
  }
}
function CatalogDraft({
  storageKey,
  handoff,
}: {
  storageKey: string;
  handoff: SnowflakeHandoff;
}): JSX.Element {
  const detectorMode = useDetectorMode();
  const sdk = useSdkClient(),
    routes = useRoutes(),
    navigate = useNavigate();
  const { orgSlug } = useSlugs();
  const cache = useQueryClient();
  const organization = useOrganization(),
    project = useProject(),
    session = useSession();
  const [params] = useSearchParams();
  const gatewayId = params.get("attachToGateway");
  const checkpointKey = `${storageKey}:installation`;
  const checkpoint = useRef(readCheckpoint(checkpointKey));
  const [s, dispatch] = useReducer(reduce, storageKey, (key) =>
    loadProviderProgress("snowflake", key),
  );
  const [server, setServer] = useState<McpServer | null>(null);
  const [serverUrl, setServerUrl] = useState("");
  const [pending, setPending] = useState(!!checkpoint.current.serverId);
  const busy = useRef(false);
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  const [error, setError] = useState<string | null>(null);
  const [saveOnMount, setSaveOnMount] = useState(false);
  const remember = useCallback(() => {
    try {
      sessionStorage.setItem(checkpointKey, JSON.stringify(checkpoint.current));
    } catch {
      /* Retain the checkpoint in memory when browser storage is unavailable. */
    }
  }, [checkpointKey]);
  const loadServer = async () => {
    const id = checkpoint.current.serverId;
    if (!id) return;
    let target = await sdk.mcpServers.get({ id });
    if (!target.remoteMcpServerId) throw new Error("Source missing");
    const upstream = await sdk.remoteMcp.getServer({
      id: target.remoteMcpServerId,
    });
    if (!snowflakeSelectionForUrl(upstream.url))
      throw new Error("Snowflake source changed");
    if (!target.platformEndpointSlug) {
      if (!orgSlug || !(await createDefaultMcpEndpoint(sdk, target, orgSlug)))
        throw new Error("Endpoint creation failed");
      target = await sdk.mcpServers.get({ id });
    }
    const fingerprint = providerSourceFingerprint(target.remoteMcpServerId, {
      url: upstream.url,
    });
    if (
      checkpoint.current.sourceFingerprint &&
      checkpoint.current.sourceFingerprint !== fingerprint
    ) {
      dispatch({
        type: "source",
        values: snowflakeSelectionForUrl(upstream.url) ?? {},
        sourceFingerprint: fingerprint,
      });
    }
    checkpoint.current.sourceFingerprint = fingerprint;
    remember();
    if (
      handoff.guardrail &&
      !handoff.customize &&
      !checkpoint.current.guardrailAttempted
    ) {
      // As in the catalog installer, policy failure leaves the accepted source
      // intact. The existing Guardrails tab is the recovery path.
      checkpoint.current.guardrailAttempted = true;
      remember();
      try {
        await sdk.risk.policies.create({
          createRiskPolicyRequestBody: buildServerGuardrailRequest(
            handoff.guardrail,
            {
              mcpServerIds: [id],
              name: `${handoff.name} guardrail`,
              mode: detectorMode,
            },
          ),
        });
      } catch {
        toast.warning(
          "Snowflake was added, but its risk policy couldn’t be created. Retry from the server’s Guardrails tab.",
        );
      }
      void invalidateAllRiskListPolicies(cache).catch(() => undefined);
    }
    setServerUrl(upstream.url);
    setServer(target);
  };
  useEffect(() => {
    if (!checkpoint.current.serverId || busy.current) return;
    busy.current = true;
    void loadServer()
      .catch(() =>
        setError(
          "Couldn’t load your saved Snowflake setup. Retry to continue with the same server.",
        ),
      )
      .finally(() => {
        busy.current = false;
        setPending(false);
      });
    // This scoped draft is mounted once; retries are explicit.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  useEffect(() => {
    saveProviderProgress(storageKey, s);
    if (server && s.sourceFingerprint) {
      checkpoint.current.sourceFingerprint = s.sourceFingerprint;
      remember();
    }
    if (server)
      saveProviderProgress(
        providerProgressKey(
          "snowflake",
          session.user.id,
          organization.id,
          project.id,
          server.id,
        ),
        s,
      );
  }, [
    storageKey,
    s,
    server,
    session.user.id,
    organization.id,
    project.id,
    remember,
  ]);
  useEffect(() => {
    window.scrollTo(0, 0);
  }, [s.step]);
  useEffect(() => {
    if (s.screen !== "success" || !server) return;
    // The next catalog Add starts a new setup, rather than reopening this one.
    try {
      sessionStorage.removeItem(storageKey);
      sessionStorage.removeItem(checkpointKey);
      sessionStorage.removeItem(`${storageKey}:handoff`);
    } catch {
      // Completion still opens the server when browser storage becomes unavailable.
    }
    void navigate(
      handoff.customize
        ? routes.policyCenter.new.href() +
            policyNewPrefillQuery({
              mcpServerIds: [server.id],
              ...(handoff.guardrail
                ? {
                    categories: handoff.guardrail.categories,
                    action: effectiveAction(handoff.guardrail),
                    score: handoff.guardrail.score,
                  }
                : {}),
              name: `${handoff.name} guardrail`,
            })
        : routes.mcp.details.overview.href(mcpServerRouteParam(server)),
      { replace: true },
    );
  }, [s.screen, server, storageKey, checkpointKey, navigate, routes, handoff]);

  const install = async () => {
    if (busy.current || pending) return;
    if (s.step !== "credentials" || Object.keys(validate(s)).length) {
      dispatch({ type: "next" });
      return;
    }
    const connection = snowflakeConnection(s.values);
    if (!connection) return;
    busy.current = true;
    setPending(true);
    setError(null);
    try {
      // Each completed write is remembered before another starts. A retry
      // reuses a partially created source rather than creating duplicates.
      if (!checkpoint.current.remoteId) {
        const remote = await sdk.remoteMcp.createServer({
          createServerForm: {
            name: handoff.name,
            url: connection.url,
            transportType: "streamable-http",
          },
        });
        checkpoint.current.remoteId = remote.id;
        remember();
      }
      if (!mounted.current) return;
      if (!checkpoint.current.serverId) {
        const created = await sdk.mcpServers.create({
          createMcpServerForm: {
            name: handoff.name,
            remoteMcpServerId: checkpoint.current.remoteId,
            visibility: "disabled",
          },
        });
        checkpoint.current.serverId = created.id;
        remember();
      }
      if (!mounted.current) return;
      await Promise.all([
        invalidateAllMcpServers(cache),
        invalidateAllRemoteMcpServers(cache),
      ]);
      if (!mounted.current) return;
      setSaveOnMount(true);
      await loadServer();
    } catch {
      setError(
        "Couldn’t prepare your Snowflake connection. Retry to continue with the same server.",
      );
    } finally {
      busy.current = false;
      setPending(false);
    }
  };
  if (server)
    return (
      <InstalledDraft
        s={s}
        dispatch={dispatch}
        server={server}
        serverUrl={serverUrl}
        saveOnMount={saveOnMount}
        serverName={handoff.name}
        gatewayId={gatewayId ?? undefined}
      />
    );
  return (
    <SetupContext
      value={{
        s,
        dispatch: (action) => {
          if (!pending) dispatch(action);
        },
        live: {
          next: () => void install(),
          changeField: (change) => {
            if (!pending) dispatch({ type: "field", ...change });
          },
          dispatch: (action) => {
            if (!pending) dispatch(action);
          },
          signIn: () => {},
          testConnection: () => {},
          cancelSignIn: () => {},
          pending: pending ? "save" : null,
          loading: !!checkpoint.current.serverId && !!error,
          error,
          errorStep: "save",
          toolCount: null,
          retryLoad: () => {
            setPending(true);
            setError(null);
            void loadServer()
              .catch(() =>
                setError(
                  "Couldn’t load your saved Snowflake setup. Try again.",
                ),
              )
              .finally(() => setPending(false));
          },
        },
      }}
    >
      <SetupBody serverName={handoff.name} serverUrl="" />
    </SetupContext>
  );
}
function InstalledDraft({
  s,
  dispatch,
  server,
  serverUrl,
  saveOnMount,
  serverName,
  gatewayId,
}: {
  s: State;
  dispatch: Dispatch<Action>;
  server: McpServer;
  serverUrl: string;
  saveOnMount: boolean;
  serverName: string;
  gatewayId?: string;
}): JSX.Element {
  const current = useGetMcpServer({ id: server.id }, undefined, {
    throwOnError: false,
  });
  const remote = useGetRemoteMcpServer(
    { id: server.remoteMcpServerId! },
    undefined,
    { throwOnError: false },
  );
  const sdk = useSdkClient();
  const queryClient = useQueryClient();
  const live = useProviderConnection(
    s,
    dispatch,
    current.data ?? server,
    {
      url: remote.data?.url ?? serverUrl,
    },
    gatewayId
      ? async () => {
          // Adding after credential save lets the existing gateway writer attach
          // the configured provider client to the gateway's issuer.
          const { members } = await sdk.metaMcp.listMembers({
            metaMcpServerId: gatewayId,
          });
          if (!members.some((member) => member.mcpServerId === server.id))
            await sdk.metaMcp.addMember({
              addMetaMcpMemberForm: {
                metaMcpServerId: gatewayId,
                mcpServerId: server.id,
              },
            });
          await invalidateAllMetaMcpMembers(queryClient);
        }
      : undefined,
  );
  const autoSave = useRef(saveOnMount);
  const routes = useRoutes();
  useEffect(() => {
    if (!autoSave.current || live.loading || live.pending || live.error) return;
    autoSave.current = false;
    live.next();
  }, [live]);
  return (
    <SetupContext
      value={{
        s,
        dispatch: live.dispatch,
        live: {
          ...live,
          settingsHref:
            routes.mcp.details.settings.href(mcpServerRouteParam(server)) +
            "#authentication",
        },
      }}
    >
      <SetupBody serverName={serverName} serverUrl={serverUrl} />
    </SetupContext>
  );
}
