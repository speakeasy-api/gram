import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type Dispatch,
} from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useSdkClient } from "@/contexts/Sdk";
import { useProject, useSession } from "@/contexts/Auth";
import { collectPageItems } from "@/components/sessions/collectPageItems";
import { firstPartyConnectUrl, getServerURL } from "@/lib/utils";
import { remoteSessionsTestServerConnection } from "@gram/client/funcs/remoteSessionsTestServerConnection";
import { unwrapAsync } from "@gram/client/types/fp";
import { isNotFoundError } from "@/lib/route-errors";
import { scopesFor } from "@/pages/catalog/provider-setup/provider-data";
import {
  validate,
  type Action,
  type State,
} from "@/pages/catalog/provider-setup/state";
import { GramError } from "@gram/client/models/errors/gramerror";
import type { McpServer } from "@gram/client/models/components/mcpserver";
import { invalidateAllGetMcpServer } from "@gram/client/react-query/getMcpServer";
import { invalidateAllRemoteSessionClients } from "@gram/client/react-query/remoteSessionClients";
import { invalidateAllRemoteSessionIssuers } from "@gram/client/react-query/remoteSessionIssuers";

const providers = {
  slack: {
    name: "Slack",
    issuer: "https://mcp.slack.com",
    authorizationEndpoint: "https://slack.com/oauth/v2_user/authorize",
    tokenEndpoint: "https://slack.com/api/oauth.v2.user.access",
  },
};

type ProviderConnection = {
  dispatch: Dispatch<Action>;
  pending: "save" | "signin" | "test" | null;
  error: string | null;
  errorStep: "save" | "signin" | "test";
  toolCount: number | null;
  loading: boolean;
  callbackUrl?: string;
  savedClientId?: string;
  recovery?: "signin" | "credentials" | "settings";
  retryLoad: () => void;
  cancelSignIn: () => void;
};

// Shared setup actions use the management API; secrets and tokens are never progress state.
export function useProviderConnection(
  s: State,
  dispatch: Dispatch<Action>,
  server: McpServer,
): ProviderConnection {
  const providerKey = "slack";
  const config = providers[providerKey];
  const name = config.name;
  const requestedScopes = useMemo(() => scopesFor(s), [providerKey, s]);
  const sdk = useSdkClient();
  const session = useSession();
  const project = useProject();
  const queryClient = useQueryClient();
  const [pending, setPending] = useState<"save" | "signin" | "test" | null>(
    null,
  );
  const [signInReady, setSignInReady] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [errorStep, setErrorStep] = useState<"save" | "signin" | "test">(
    "save",
  );
  const [toolCount, setToolCount] = useState<number | null>(null);
  const [recovery, setRecovery] = useState<ProviderConnection["recovery"]>();
  const popup = useRef<Window | null>(null);
  const generation = useRef(0);
  const hydrated = useRef(false);
  const statusKey = [
    "providerSetupConnection",
    providerKey,
    server.id,
    server.userSessionIssuerId,
    session.user.id,
  ];
  const status = useQuery({
    queryKey: statusKey,
    queryFn: async () => {
      const { associations } =
        await sdk.remoteSessions.listServerClientAssociations({
          mcpServerId: server.id,
        });
      const association = associations.find(
        (a) => a.issuerUrl === config.issuer,
      );
      if (!association) return null;
      const client = await sdk.remoteSessionClients.get({
        id: association.remoteSessionClientId,
      });
      return {
        client,
        serverCount: association.serverCount,
      };
    },
    retry: false,
    refetchOnWindowFocus: false,
    throwOnError: false,
  });
  useEffect(() => {
    if (!status.isSuccess || hydrated.current) return;
    hydrated.current = true;
    if (status.data) {
      if (providerKey === "slack")
        dispatch({
          type: "field",
          field: "access",
          value: status.data.client.scope?.includes("chat:write")
            ? "all"
            : "read",
        });
      dispatch({
        type: "connection",
        saved: true,
        authorized: false,
        tested: false,
      });
    }
  }, [status.isSuccess, status.data, dispatch, providerKey]);

  const checkConnection = useCallback(
    () =>
      unwrapAsync(
        remoteSessionsTestServerConnection(
          sdk,
          { testServerConnectionRequestBody: { mcpServerId: server.id } },
          {
            projectSlugHeaderGramProject: project.slug,
            sessionHeaderGramSession: session.session,
          },
          { timeoutMs: 30000 },
        ),
      ),
    [sdk, server.id, project.slug, session.session],
  );

  useEffect(() => {
    if (pending !== "signin" || !signInReady) return;
    let active = true;
    let checking = false;
    const check = async () => {
      if (checking) return;
      checking = true;
      try {
        const result = await checkConnection();
        if (!active) return;
        const authorized =
          result.status === "ready" && result.toolCount != null;
        dispatch({ type: "connection", authorized, tested: false });
        if (authorized) {
          setPending(null);
          setError(null);
        } else if (result.status !== "authorization_required") {
          setPending(null);
          setError(
            result.message || `Couldn’t verify ${name} sign-in. Try again.`,
          );
          if (result.status === "configuration_required")
            setRecovery("settings");
        } else if (popup.current?.closed) {
          setPending(null);
          setError(`${name} sign-in did not finish. Try signing in again.`);
        }
      } catch {
        if (!active) return;
        setPending(null);
        setError(`Couldn’t check ${name} sign-in. Try again.`);
      } finally {
        checking = false;
      }
    };
    void check();
    const timer = window.setInterval(() => void check(), 2000);
    const timeout = window.setTimeout(
      () => {
        active = false;
        setPending(null);
        setError(`${name} sign-in timed out. Try signing in again.`);
      },
      5 * 60 * 1000,
    );
    window.addEventListener("focus", check);
    return () => {
      active = false;
      window.clearInterval(timer);
      window.clearTimeout(timeout);
      window.removeEventListener("focus", check);
    };
  }, [pending, signInReady, name, checkConnection, dispatch]);

  // Revoke before changing scopes: a failed update must not leave a broader
  // grant usable, and verification must wait until both operations finish.
  const syncPermissions = async (scopes: string[]) => {
    const saved = status.data;
    if (
      providerKey !== "slack" ||
      !saved ||
      scopes.join(" ") === saved.client.scope?.join(" ")
    )
      return;
    if (saved.serverCount > 1) {
      setRecovery("settings");
      throw new Error("Shared app connection");
    }
    const sessions = await collectPageItems(
      sdk.remoteSessions.list({
        remoteSessionClientId: saved.client.id,
        subjectUrn: `user:${session.user.id}`,
      }),
    );
    for (const item of sessions) {
      if (item.userSessionIssuerId === server.userSessionIssuerId)
        await sdk.remoteSessions.revoke({ id: item.id });
    }
    await sdk.remoteSessionClients.update({
      updateRemoteSessionClientForm: { id: saved.client.id, scope: scopes },
    });
    queryClient.setQueryData(statusKey, {
      ...saved,
      client: { ...saved.client, scope: scopes },
    });
    await invalidateAllRemoteSessionClients(queryClient);
  };

  const applyPermissions = async (scopes: string[], next?: Action) => {
    setErrorStep("save");
    setPending("save");
    try {
      await syncPermissions(scopes);
      if (next) dispatch(next);
    } catch {
      setError("Couldn’t update Slack permissions. Select Continue to retry.");
    } finally {
      setPending(null);
    }
  };

  const save = async () => {
    if (s.saved && !s.values.clientId && !s.values.clientSecret) {
      dispatch({ type: "goto", step: "test" });
      return;
    }
    if (Object.keys(validate(s)).length) {
      dispatch({ type: "next" });
      return;
    }
    setErrorStep("save");
    setPending("save");
    setError(null);
    try {
      let current = await sdk.mcpServers.get({ id: server.id });
      const issuer = current.userSessionIssuerId
        ? await sdk.userSessionIssuers.get({ id: current.userSessionIssuerId })
        : undefined;
      if (!issuer?.projectId) {
        // Catalog installs can share the organization's issuer. Keep this demo's
        // app binding on its own issuer, without repointing other servers.
        const slug = `${providerKey}-setup-${server.id.slice(-12)}`;
        let isolated;
        try {
          isolated = await sdk.userSessionIssuers.get({ slug });
        } catch (err) {
          if (!isNotFoundError(err)) throw err;
        }
        isolated ??= await sdk.userSessionIssuers.create({
          createUserSessionIssuerForm: {
            slug,
            sessionDurationHours: 24,
            authnChallengeMode: "interactive",
          },
        });
        current = await sdk.mcpServers.update({
          updateMcpServerForm: {
            id: current.id,
            visibility:
              current.visibility === "disabled"
                ? "private"
                : current.visibility === "public"
                  ? "public"
                  : "private",
            remoteMcpServerId: current.remoteMcpServerId,
            environmentId: current.environmentId,
            toolVariationsGroupId: current.toolVariationsGroupId,
            userSessionIssuerId: isolated.id,
          },
        });
        await invalidateAllGetMcpServer(queryClient);
      }
      let provider;
      try {
        provider = await sdk.remoteSessionIssuers.get({
          issuer: config.issuer,
        });
      } catch (err) {
        if (!isNotFoundError(err)) throw err;
      }
      const result = await sdk.remoteSessions.commitServerIdentityConfiguration(
        {
          commitServerIdentityConfigurationForm: {
            mcpServerId: server.id,
            targetServerOnly: true,
            providerId: provider?.id,
            createProvider: provider
              ? undefined
              : {
                  issuer: config.issuer,
                  slug: `${providerKey}-${server.id.slice(0, 8)}`,
                  name,
                  authorizationEndpoint: config.authorizationEndpoint,
                  tokenEndpoint: config.tokenEndpoint,
                  authorizationResponseIssParameterSupported: false,
                  tokenEndpointAuthMethodsSupported: ["client_secret_post"],
                  responseTypesSupported: ["code"],
                  codeChallengeMethodsSupported: ["S256"],
                  grantTypesSupported: ["authorization_code", "refresh_token"],
                },
            clientMode: "manual",
            clientConfiguration: {
              clientId: s.values.clientId.trim(),
              clientSecret: s.values.clientSecret.trim(),
              tokenEndpointAuthMethod: "client_secret_post",
              scope: requestedScopes,
            },
          },
        },
      );
      if (!result.status)
        throw new Error(`Couldn’t save ${name} app credentials.`);
      // A disabled catalog install must serve its private OAuth endpoint before sign-in.
      if (current.visibility === "disabled")
        await sdk.mcpServers.update({
          updateMcpServerForm: {
            id: current.id,
            visibility: "private",
            remoteMcpServerId: current.remoteMcpServerId,
            environmentId: current.environmentId,
            toolVariationsGroupId: current.toolVariationsGroupId,
          },
        });
      dispatch({ type: "field", field: "clientId", value: "" });
      dispatch({ type: "field", field: "clientSecret", value: "" });
      dispatch({
        type: "connection",
        saved: true,
        authorized: false,
        tested: false,
      });
      dispatch({ type: "goto", step: "test" });
      await Promise.all([
        status.refetch(),
        invalidateAllGetMcpServer(queryClient),
        invalidateAllRemoteSessionClients(queryClient),
        invalidateAllRemoteSessionIssuers(queryClient),
      ]);
    } catch (err) {
      const identityConflict =
        err instanceof GramError && err.statusCode === 409;
      setRecovery(identityConflict ? "settings" : undefined);
      setError(
        identityConflict
          ? "Review this server’s identity settings before saving these app credentials."
          : `Couldn’t save ${name} credentials. Check the app details and try again.`,
      );
    } finally {
      setPending(null);
    }
  };

  const signIn = async () => {
    const run = generation.current;
    setErrorStep("signin");
    setRecovery(undefined);
    const slug = server.platformEndpointSlug;
    if (!slug) {
      setError("This server needs an endpoint before you can sign in.");
      return;
    }
    // Open synchronously so browsers allow the tab before any await.
    popup.current = window.open("about:blank", `${providerKey}-setup-signin`);
    if (!popup.current) {
      setError(`Allow pop-ups to sign in to ${name}.`);
      return;
    }
    setSignInReady(false);
    setPending("signin");
    setError(null);
    dispatch({ type: "connection", authorized: false, tested: false });
    try {
      const savedClient = status.data?.client;
      if (!savedClient) {
        popup.current?.close();
        setPending(null);
        setRecovery("credentials");
        setError(`Save your ${name} app credentials first.`);
        return;
      }
      if (providerKey === "slack") await syncPermissions(requestedScopes);
      if (generation.current !== run) {
        setPending(null);
        return;
      }
      const url = firstPartyConnectUrl(`${getServerURL()}/mcp/${slug}`);
      if (!url) throw new Error(`Couldn’t open ${name} sign-in.`);
      popup.current.location.href = url;
      setSignInReady(true);
    } catch {
      if (generation.current !== run) {
        setPending(null);
        return;
      }
      popup.current?.close();
      setPending(null);
      setError(`Couldn’t start ${name} sign-in. Try again.`);
    }
  };

  const test = async () => {
    const run = generation.current;
    setErrorStep("test");
    setPending("test");
    setError(null);
    setToolCount(null);
    dispatch({ type: "connection", tested: false });
    try {
      const result = await checkConnection();
      if (generation.current !== run) return;
      if (result.status === "ready" && result.toolCount != null) {
        setToolCount(result.toolCount);
        dispatch({ type: "connection", authorized: true, tested: true });
      } else {
        if (result.status === "authorization_required") {
          dispatch({ type: "connection", authorized: false, tested: false });
          setRecovery("signin");
        } else if (result.status === "configuration_required") {
          setRecovery(
            result.code === "remote_user_identity_required"
              ? "settings"
              : "credentials",
          );
        }
        setError(result.message || `Couldn’t test ${name}. Try again.`);
      }
    } catch {
      if (generation.current === run)
        setError(
          `Couldn’t initialize ${name} and list its tools. Check app permissions, sign in again, and retry.`,
        );
    } finally {
      setPending(null);
    }
  };

  const liveDispatch: Dispatch<Action> = (action) => {
    if (pending && !["secret", "copy"].includes(action.type)) return;
    setError(null);
    setRecovery(undefined);
    if (providerKey === "slack" && s.saved) {
      if (
        action.type === "field" &&
        action.field === "access" &&
        action.value !== s.values.access
      ) {
        if ((status.data?.serverCount ?? 0) > 1) {
          setErrorStep("save");
          setRecovery("settings");
          setError(
            "This app connection is shared. Update its permissions in Settings.",
          );
          return;
        }
        generation.current++;
        setToolCount(null);
        dispatch(action);
        void applyPermissions(
          scopesFor({ ...s, values: { ...s.values, access: action.value } }),
        );
        return;
      }
      if (
        action.type === "next" &&
        requestedScopes.join(" ") !== status.data?.client.scope?.join(" ")
      ) {
        void applyPermissions(
          requestedScopes,
          s.step === "credentials" ? { type: "goto", step: "test" } : action,
        );
        return;
      }
    }
    if (action.type === "next" && s.step === "credentials") {
      void save();
      return;
    }
    if (action.type === "modal" && action.modal === "signin") {
      void signIn();
      return;
    }
    if (action.type === "modal" && action.modal === "test") {
      void test();
      return;
    }
    if (action.type === "field" || action.type === "restart") {
      generation.current++;
      setToolCount(null);
    }
    dispatch(action);
  };
  return {
    dispatch: liveDispatch,
    pending,
    error:
      error ??
      (status.error
        ? `Couldn’t load saved ${name} credentials. Try again.`
        : null),
    errorStep: error ? errorStep : "save",
    recovery,
    retryLoad: () => {
      void status.refetch();
    },
    cancelSignIn: () => {
      generation.current++;
      popup.current?.close();
      setSignInReady(false);
      // An in-flight scope mutation must settle before another choice can write.
      setPending(pending === "signin" && !signInReady ? "save" : null);
      dispatch({ type: "connection", authorized: false, tested: false });
    },
    toolCount,
    loading: status.isLoading || status.isError,
    callbackUrl: status.data?.client.callbackUrl,
    savedClientId: status.data?.client.clientId,
  };
}
