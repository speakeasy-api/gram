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
import { invalidateAllRemoteSessions } from "@gram/client/react-query/remoteSessions";
import { invalidateAllRemoteSessionsCount } from "@gram/client/react-query/remoteSessionsCount";
import { invalidateAllUserSessionIssuer } from "@gram/client/react-query/userSessionIssuer";
import { invalidateAllRemoteSessionClients } from "@gram/client/react-query/remoteSessionClients";
import { invalidateAllRemoteSessionIssuers } from "@gram/client/react-query/remoteSessionIssuers";

import { invalidateAllMcpServers } from "@gram/client/react-query/mcpServers";
import { invalidateAllUserSessionIssuers } from "@gram/client/react-query/userSessionIssuers";
import { invalidateAllRemoteSessionClient } from "@gram/client/react-query/remoteSessionClient";
import { invalidateAllRemoteSessionIssuer } from "@gram/client/react-query/remoteSessionIssuer";
import { invalidateAllGetRemoteMcpServerScopes } from "@gram/client/react-query/getRemoteMcpServerScopes";

const sameScopes = (
  left: readonly string[] | undefined,
  right: readonly string[],
) => {
  const a = [...new Set(left ?? [])].sort(),
    b = [...new Set(right)].sort();
  return a.length === b.length && a.every((scope, i) => scope === b[i]);
};

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
  recovery?: "signin" | "credentials" | "settings" | "permissions";
  retryLoad: () => void;
  cancelSignIn: () => void;
};

function connectionFailure(
  result: { status: string; code: string },
  action: "signin" | "test",
): string {
  if (result.code === "scope_mismatch")
    return "Slack is still granting write access. Remove write scopes and revoke the app’s existing authorization in Slack, then sign in again.";
  if (result.code === "authorization_header_conflict")
    return "Remove the saved Authorization header in identity settings before testing User Identity.";
  if (result.status === "configuration_required")
    return "Review this server’s User Identity configuration before continuing.";
  if (result.status === "authorization_required")
    return "Sign in to Slack, then test the connection again.";
  return action === "signin"
    ? "Couldn’t verify Slack sign-in. Try again."
    : "Couldn’t test the Slack connection. Review app permissions and try again.";
}

// Shared setup actions use the management API; secrets and tokens are never progress state.
export function useProviderConnection(
  s: State,
  dispatch: Dispatch<Action>,
  server: McpServer,
): ProviderConnection {
  const providerKey = "slack";
  const config = providers[providerKey];
  const name = config.name;
  const access = s.values.access;
  const requestedScopes = useMemo(
    () => scopesFor({ values: { access } }),
    [access],
  );
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
  useEffect(
    () => () => {
      generation.current++;
      popup.current?.close();
    },
    [],
  );
  const targetKey = JSON.stringify([
    project.id,
    server.id,
    server.userSessionIssuerId,
    server.remoteMcpServerId,
    server.environmentId,
    server.updatedAt,
  ]);
  const currentTarget = useRef(targetKey);
  const observedClient = useRef<string | null>(null);
  const statusKey = [
    "providerSetupConnection",
    targetKey,
    providerKey,
    server.id,
    server.userSessionIssuerId,
    session.user.id,
  ];
  const loadConfiguration = async () => {
    const { associations } =
      await sdk.remoteSessions.listServerClientAssociations({
        mcpServerId: server.id,
      });
    const association = associations.find((a) => {
      try {
        const url = new URL(a.issuerUrl);
        return (
          url.origin === config.issuer &&
          url.pathname.replace(/\/+$/, "") === "" &&
          !url.search &&
          !url.hash &&
          !url.username &&
          !url.password
        );
      } catch {
        return false;
      }
    });
    if (!association) return null;
    const client = await sdk.remoteSessionClients.get({
      id: association.remoteSessionClientId,
    });
    return {
      client,
      serverCount: association.serverCount,
    };
  };
  const status = useQuery({
    queryKey: statusKey,
    queryFn: loadConfiguration,
    retry: false,
    refetchOnWindowFocus: true,
    throwOnError: false,
  });
  useEffect(() => {
    if (currentTarget.current === targetKey) return;
    currentTarget.current = targetKey;
    hydrated.current = false;
    observedClient.current = null;
    generation.current++;
    setToolCount(null);
    popup.current?.close();
    setSignInReady(false);
    setPending(
      pending === "save" || (pending === "signin" && !signInReady)
        ? "save"
        : null,
    );
    dispatch({
      type: "connection",
      saved: false,
      authorized: false,
      tested: false,
    });
  }, [targetKey, pending, signInReady, dispatch]);
  useEffect(() => {
    if (!status.isSuccess) return;
    const fingerprint = JSON.stringify([
      status.data?.client.id,
      status.data?.client.scope,
      status.data?.client.updatedAt,
    ]);
    if (
      observedClient.current !== null &&
      observedClient.current !== fingerprint
    ) {
      setToolCount(null);
      dispatch({
        type: "connection",
        authorized: false,
        tested: false,
        ...(!status.data ? { saved: false } : {}),
      });
      if (pending !== "save" && !(pending === "signin" && !signInReady)) {
        generation.current++;
        popup.current?.close();
        setSignInReady(false);
        setPending(null);
      }
    }
    observedClient.current = fingerprint;
  }, [status.isSuccess, status.data, pending, signInReady, dispatch]);
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

  const revokeSessions = useCallback(
    async (items: { id: string }[], run = generation.current) => {
      if (!items.length) return;
      try {
        for (const item of items) {
          if (generation.current !== run)
            throw new Error("Setup target changed");
          await sdk.remoteSessions.revoke({ id: item.id });
        }
      } finally {
        await Promise.all([
          invalidateAllRemoteSessions(queryClient),
          invalidateAllRemoteSessionsCount(queryClient),
        ]);
      }
    },
    [sdk, queryClient],
  );

  const checkConnection = useCallback(async () => {
    const run = generation.current;
    const result = await unwrapAsync(
      remoteSessionsTestServerConnection(
        sdk,
        { testServerConnectionRequestBody: { mcpServerId: server.id } },
        {
          projectSlugHeaderGramProject: project.slug,
          sessionHeaderGramSession: session.session,
        },
        { timeoutMs: 30000 },
      ),
    );
    // Slack scopes are additive. Never accept a returned write grant as read-only.
    if (
      result.status === "ready" &&
      s.values.access === "read" &&
      status.data?.client.id
    ) {
      const sessions = await collectPageItems(
        sdk.remoteSessions.list({
          remoteSessionClientId: status.data.client.id,
        }),
      );
      const broader = sessions.filter(
        (item) =>
          item.userSessionIssuerId === server.userSessionIssuerId &&
          item.scopes?.some((scope) => scope.includes(":write")),
      );
      if (broader.length) {
        await revokeSessions(broader, run);
        return {
          status: "authorization_required" as const,
          code: "scope_mismatch",
          message: "",
          toolCount: undefined,
        };
      }
    }
    return result;
  }, [
    sdk,
    server.id,
    server.userSessionIssuerId,
    project.slug,
    session.session,
    s.values.access,
    status.data?.client.id,
    revokeSessions,
  ]);

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
        } else if (result.code === "scope_mismatch") {
          popup.current?.close();
          setPending(null);
          setRecovery("permissions");
          setError(connectionFailure(result, "signin"));
        } else if (result.status !== "authorization_required") {
          setPending(null);
          setError(connectionFailure(result, "signin"));
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
        generation.current++;
        popup.current?.close();
        setPending(null);
        setError(`${name} sign-in timed out. Try signing in again.`);
      },
      5 * 60 * 1000,
    );
    const onFocus = () => {
      void check();
    };
    window.addEventListener("focus", onFocus);
    return () => {
      active = false;
      window.clearInterval(timer);
      window.clearTimeout(timeout);
      window.removeEventListener("focus", onFocus);
    };
  }, [pending, signInReady, name, checkConnection, dispatch]);

  // Revoke before changing scopes: a failed update must not leave a broader
  // grant usable, and verification must wait until both operations finish.
  const syncPermissions = async (scopes: string[]) => {
    const run = generation.current;
    const saved = await loadConfiguration();
    if (generation.current !== run) throw new Error("Setup target changed");
    if (!saved) throw new Error("Saved app configuration is missing");
    if (saved.client.id !== status.data?.client.id) {
      setRecovery("settings");
      throw new Error("App configuration changed");
    }
    const changing = !sameScopes(saved.client.scope, scopes);
    const readOnly = !scopes.some((scope) => scope.includes(":write"));
    if (!changing && !readOnly) return;
    if (saved.serverCount > 1 && changing) {
      setRecovery("settings");
      throw new Error("Shared app connection");
    }
    const sessions = await collectPageItems(
      sdk.remoteSessions.list({
        remoteSessionClientId: saved.client.id,
      }),
    );
    if (generation.current !== run) throw new Error("Setup target changed");
    await revokeSessions(
      sessions.filter(
        (item) =>
          item.userSessionIssuerId === server.userSessionIssuerId &&
          (changing || item.scopes?.some((scope) => scope.includes(":write"))),
      ),
    );
    if (generation.current !== run) throw new Error("Setup target changed");
    if (!changing) return;
    const updatedClient = await sdk.remoteSessionClients.update({
      updateRemoteSessionClientForm: { id: saved.client.id, scope: scopes },
    });
    if (generation.current !== run) throw new Error("Setup target changed");
    const updated = {
      ...saved,
      client: updatedClient ?? { ...saved.client, scope: scopes },
    };
    observedClient.current = JSON.stringify([
      updated.client.id,
      updated.client.scope,
      updated.client.updatedAt,
    ]);
    queryClient.setQueryData(statusKey, updated);
    await Promise.all([
      invalidateAllRemoteSessionClients(queryClient),
      invalidateAllRemoteSessionClient(queryClient),
      invalidateAllGetRemoteMcpServerScopes(queryClient),
    ]);
  };

  const applyPermissions = async (
    scopes: string[],
    next?: Action | (() => Promise<void>),
  ) => {
    const run = generation.current;
    setErrorStep("save");
    setPending("save");
    try {
      await syncPermissions(scopes);
      if (generation.current !== run) return;
      if (typeof next === "function") await next();
      else if (next) dispatch(next);
    } catch {
      if (generation.current === run)
        setError(
          "Couldn’t update Slack permissions. Select Continue to retry.",
        );
    } finally {
      setPending(null);
    }
  };

  const save = async () => {
    const run = generation.current;
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
      if (generation.current !== run) return;
      const issuer = current.userSessionIssuerId
        ? await sdk.userSessionIssuers.get({ id: current.userSessionIssuerId })
        : undefined;
      if (generation.current !== run) return;
      if (!issuer?.projectId) {
        // Catalog installs can share the organization's issuer. Keep this server's
        // app binding on a dedicated issuer before saving its credentials.
        const slug = `${providerKey}-setup-${server.id.slice(-12)}`;
        let isolated;
        try {
          isolated = await sdk.userSessionIssuers.get({ slug });
        } catch (err) {
          if (!isNotFoundError(err)) throw err;
        }
        if (generation.current !== run) return;
        isolated ??= await sdk.userSessionIssuers.create({
          createUserSessionIssuerForm: {
            slug,
            sessionDurationHours: 24,
            authnChallengeMode: "interactive",
          },
        });
        if (generation.current !== run) return;
        current = await sdk.mcpServers.update({
          updateMcpServerForm: {
            id: current.id,
            visibility:
              current.visibility === "disabled"
                ? "disabled"
                : current.visibility === "public"
                  ? "public"
                  : "private",
            remoteMcpServerId: current.remoteMcpServerId,
            environmentId: current.environmentId,
            toolVariationsGroupId: current.toolVariationsGroupId,
            userSessionIssuerId: isolated.id,
          },
        });
      }
      let provider;
      try {
        provider = await sdk.remoteSessionIssuers.get({
          issuer: config.issuer,
        });
      } catch (err) {
        if (!isNotFoundError(err)) throw err;
      }
      if (generation.current !== run) return;
      // Re-read immediately before committing: another editor may have changed
      // the identity while provider metadata was loading.
      const latest = await sdk.mcpServers.get({ id: server.id });
      if (generation.current !== run) return;
      if (
        latest.userSessionIssuerId !== current.userSessionIssuerId ||
        latest.remoteMcpServerId !== current.remoteMcpServerId ||
        latest.environmentId !== current.environmentId
      ) {
        setRecovery("settings");
        setError(
          "This server’s identity changed. Reload its settings before saving credentials.",
        );
        return;
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
      if (generation.current !== run) return;
      if (result.status !== "registered" && result.status !== "linked")
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
      if (generation.current !== run) return;
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
        invalidateAllRemoteSessionIssuer(queryClient),
        invalidateAllRemoteSessionClient(queryClient),
        invalidateAllMcpServers(queryClient),
        invalidateAllUserSessionIssuers(queryClient),
        invalidateAllUserSessionIssuer(queryClient),
        invalidateAllGetRemoteMcpServerScopes(queryClient),
      ]);
    } catch (err) {
      if (generation.current !== run) return;
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
    try {
      const address = new URL(getServerURL());
      const local = ["localhost", "127.0.0.1", "[::1]"].includes(
        address.hostname,
      );
      if (
        address.protocol !== "https:" &&
        !(address.protocol === "http:" && local)
      )
        throw new Error("Insecure address");
    } catch {
      setError("Use an HTTPS server address before signing in to Slack.");
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
          setRecovery(
            result.code === "scope_mismatch" ? "permissions" : "signin",
          );
        } else if (result.status === "configuration_required") {
          setRecovery(
            result.code === "remote_user_identity_required"
              ? "settings"
              : "credentials",
          );
        }
        setError(connectionFailure(result, "test"));
      }
    } catch {
      if (generation.current === run)
        setError(
          `Couldn’t initialize ${name} and list its tools. Check app permissions, sign in again, and retry.`,
        );
    } finally {
      if (generation.current === run) setPending(null);
    }
  };

  const liveDispatch: Dispatch<Action> = (action) => {
    if (pending && !["secret", "copy"].includes(action.type)) return;
    if (
      action.type === "complete" &&
      (status.isLoading || status.isError || error)
    )
      return;
    if (!["secret", "copy"].includes(action.type)) {
      setError(null);
      setRecovery(undefined);
    }
    if (status.data?.client) {
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
        !sameScopes(status.data?.client.scope, requestedScopes)
      ) {
        void applyPermissions(
          requestedScopes,
          s.step === "credentials"
            ? s.saved
              ? { type: "goto", step: "test" }
              : save
            : action,
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
