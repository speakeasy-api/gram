import { useServerClientConfiguration } from "@/lib/remote-identity/queries/useServerClientConfiguration";
import { invalidateIdentityConfiguration } from "@/lib/remote-identity/queries/invalidateIdentityConfiguration";
import { mcpServerVisibilityUpdateForm } from "@/lib/mcp-server-visibility";
import { useOAuthPopup } from "@/components/sessions/useOAuthPopup";
import { queryKeyRemoteSessionClient } from "@gram/client/react-query/remoteSessionClient";
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type Dispatch,
} from "react";
import { useQueryClient } from "@tanstack/react-query";
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
  type FieldName,
} from "@/pages/catalog/provider-setup/state";
import { GramError } from "@gram/client/models/errors/gramerror";
import type { McpServer } from "@gram/client/models/components/mcpserver";
import { invalidateAllGetMcpServer } from "@gram/client/react-query/getMcpServer";
import { invalidateAllRemoteSessions } from "@gram/client/react-query/remoteSessions";
import { invalidateAllRemoteSessionsCount } from "@gram/client/react-query/remoteSessionsCount";
import { invalidateAllUserSessionIssuer } from "@gram/client/react-query/userSessionIssuer";
import { invalidateAllRemoteSessionClients } from "@gram/client/react-query/remoteSessionClients";

import { invalidateAllMcpServers } from "@gram/client/react-query/mcpServers";
import { invalidateAllUserSessionIssuers } from "@gram/client/react-query/userSessionIssuers";
import { invalidateAllRemoteSessionClient } from "@gram/client/react-query/remoteSessionClient";
import { invalidateAllGetRemoteMcpServerScopes } from "@gram/client/react-query/getRemoteMcpServerScopes";

const sameScopes = (
  left: readonly string[] | undefined,
  right: readonly string[],
) => {
  const a = [...new Set(left ?? [])].sort(),
    b = [...new Set(right)].sort();
  return a.length === b.length && a.every((scope, i) => scope === b[i]);
};

class ConfigurationChanged extends Error {}

function recoveryFor(result: {
  status: string;
  code: string;
}): ProviderConnection["recovery"] {
  if (result.status === "authorization_required")
    return result.code === "scope_mismatch" ? "permissions" : "signin";
  if (result.status === "configuration_required")
    return result.code === "remote_user_identity_required"
      ? "settings"
      : "credentials";
}

const providers = {
  slack: {
    name: "Slack",
    issuer: "https://mcp.slack.com",
    authorizationEndpoint: "https://slack.com/oauth/v2_user/authorize",
    tokenEndpoint: "https://slack.com/api/oauth.v2.user.access",
  },
};

export type ProviderConnection = {
  next: () => void;
  signIn: () => void;
  testConnection: () => void;
  changeField: (change: { field: FieldName; value: string }) => void;
  dispatch: Dispatch<Exclude<Action, { type: "field" | "next" }>>;
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
  const [phase, setPhase] = useState<
    "idle" | "saving" | "preparing-signin" | "awaiting-signin" | "testing"
  >("idle");
  const [failure, setFailure] = useState<{
    step: ProviderConnection["errorStep"];
    message: string;
    recovery?: ProviderConnection["recovery"];
  } | null>(null);
  const [toolCount, setToolCount] = useState<number | null>(null);
  const pending =
    phase === "idle"
      ? null
      : phase === "saving"
        ? "save"
        : phase === "testing"
          ? "test"
          : "signin";
  const preparing = phase === "saving" || phase === "preparing-signin";
  const fail = (
    step: ProviderConnection["errorStep"],
    message: string,
    recovery?: ProviderConnection["recovery"],
  ) => setFailure({ step, message, recovery });
  const checkOnReturn = useRef<() => void>(() => {});
  const {
    cancel: cancelPopup,
    isClosed: isPopupClosed,
    open: openPopup,
  } = useOAuthPopup(() => checkOnReturn.current());
  const generation = useRef(0);
  const hydrated = useRef(false);
  useEffect(
    () => () => {
      generation.current++;
      cancelPopup();
    },
    [cancelPopup],
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
  const [loadedTarget, setLoadedTarget] = useState(targetKey);
  const configuration = useServerClientConfiguration(server.id, config.issuer);
  const loadConfiguration = configuration.refetch;
  const status = {
    ...configuration,
    isSuccess: loadedTarget === targetKey && configuration.isSuccess,
    isLoading: loadedTarget !== targetKey || configuration.isLoading,
  };
  useEffect(() => {
    if (currentTarget.current === targetKey) return;
    currentTarget.current = targetKey;
    void loadConfiguration()
      .finally(() => {
        if (currentTarget.current === targetKey) setLoadedTarget(targetKey);
      })
      .catch(() => {});
    hydrated.current = false;
    observedClient.current = null;
    generation.current++;
    setToolCount(null);
    cancelPopup();
    setPhase(preparing ? "saving" : "idle");
    dispatch({
      type: "connection",
      saved: false,
      authorized: false,
      tested: false,
    });
  }, [targetKey, preparing, dispatch, loadConfiguration, cancelPopup]);
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
      if (!preparing) {
        generation.current++;
        cancelPopup();
        setPhase("idle");
      }
    }
    observedClient.current = fingerprint;
  }, [status.isSuccess, status.data, preparing, dispatch, cancelPopup]);
  useEffect(() => {
    if (!status.isSuccess || hydrated.current) return;
    hydrated.current = true;
    if (status.data) {
      if (providerKey === "slack" && !s.accessChosen)
        dispatch({
          type: "field",
          field: "access",
          source: "configuration",
          value: status.data.client.scope?.includes("chat:write")
            ? "all"
            : "read",
        });
      dispatch({
        type: "connection",
        saved: true,
        authorized: false,
        tested: false,
        configChanged:
          s.accessChosen &&
          !sameScopes(status.data.client.scope, requestedScopes),
      });
    }
  }, [
    status.isSuccess,
    status.data,
    dispatch,
    providerKey,
    s.accessChosen,
    requestedScopes,
  ]);

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
    if (phase !== "awaiting-signin") return;
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
          setPhase("idle");
          setFailure(null);
        } else if (result.code === "scope_mismatch") {
          cancelPopup();
          setPhase("idle");
          fail("signin", connectionFailure(result, "signin"), "permissions");
        } else if (result.status !== "authorization_required") {
          setPhase("idle");
          fail(
            "signin",
            connectionFailure(result, "signin"),
            result.status === "configuration_required" ? "settings" : undefined,
          );
        } else if (isPopupClosed()) {
          setPhase("idle");
          fail(
            "signin",
            `${name} sign-in did not finish. Try signing in again.`,
          );
        }
      } catch {
        if (!active) return;
        setPhase("idle");
        fail("signin", `Couldn’t check ${name} sign-in. Try again.`);
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
        cancelPopup();
        setPhase("idle");
        fail("signin", `${name} sign-in timed out. Try signing in again.`);
      },
      5 * 60 * 1000,
    );
    checkOnReturn.current = () => void check();
    return () => {
      active = false;
      window.clearInterval(timer);
      window.clearTimeout(timeout);
      checkOnReturn.current = () => {};
    };
  }, [phase, name, checkConnection, dispatch, cancelPopup, isPopupClosed]);

  // Revoke before changing scopes: a failed update must not leave a broader
  // grant usable, and verification must wait until both operations finish.
  const syncPermissions = async (scopes: string[]) => {
    const run = generation.current;
    const saved = await loadConfiguration();
    if (generation.current !== run) throw new Error("Setup target changed");
    if (!saved) throw new Error("Saved app configuration is missing");
    if (saved.client.id !== status.data?.client.id) {
      throw new ConfigurationChanged();
    }
    const changing = !sameScopes(saved.client.scope, scopes);
    const readOnly = !scopes.some((scope) => scope.includes(":write"));
    if (!changing && !readOnly) return;
    if (saved.serverCount > 1 && changing) {
      throw new ConfigurationChanged();
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
    queryClient.setQueryData(
      queryKeyRemoteSessionClient({ id: updated.client.id }),
      updated.client,
    );
    await Promise.all([
      invalidateAllRemoteSessionClients(queryClient),
      invalidateAllRemoteSessionClient(queryClient),
      invalidateAllGetRemoteMcpServerScopes(queryClient),
    ]);
  };

  const updatePermissions = async (scopes: string[]) => {
    const run = generation.current;
    setPhase("saving");
    try {
      await syncPermissions(scopes);
      return generation.current === run;
    } catch (err) {
      if (generation.current === run)
        fail(
          "save",
          "Couldn’t update Slack permissions. Select Continue to retry.",
          err instanceof ConfigurationChanged ? "settings" : undefined,
        );
      return false;
    } finally {
      setPhase("idle");
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
    setPhase("saving");
    setFailure(null);
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
            ...mcpServerVisibilityUpdateForm(current, current.visibility),
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
        fail(
          "save",
          "This server’s identity changed. Reload its settings before saving credentials.",
          "settings",
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
            ...mcpServerVisibilityUpdateForm(current, "private"),
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
        invalidateIdentityConfiguration(queryClient),
        invalidateAllGetMcpServer(queryClient),
        invalidateAllMcpServers(queryClient),
        invalidateAllUserSessionIssuers(queryClient),
        invalidateAllUserSessionIssuer(queryClient),
      ]);
    } catch (err) {
      if (generation.current !== run) return;
      const identityConflict =
        err instanceof GramError && err.statusCode === 409;
      fail(
        "save",
        identityConflict
          ? "Review this server’s identity settings before saving these app credentials."
          : `Couldn’t save ${name} credentials. Check the app details and try again.`,
        identityConflict ? "settings" : undefined,
      );
    } finally {
      setPhase("idle");
    }
  };

  const signIn = async () => {
    const run = generation.current;
    if (pending) return;
    setFailure(null);
    const slug = server.platformEndpointSlug;
    if (!slug) {
      fail("signin", "This server needs an endpoint before you can sign in.");
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
      fail("signin", "Use an HTTPS server address before signing in to Slack.");
      return;
    }
    // A previous verification failure can leave its OAuth tab open.
    cancelPopup();
    setPhase("preparing-signin");
    setFailure(null);
    dispatch({ type: "connection", authorized: false, tested: false });
    const opened = openPopup(
      async () => {
        try {
          if (!status.data?.client) {
            fail(
              "signin",
              `Save your ${name} app credentials first.`,
              "credentials",
            );
            throw new Error("Missing credentials");
          }
          await syncPermissions(requestedScopes);
          if (generation.current !== run)
            throw new Error("Setup target changed");
          const url = firstPartyConnectUrl(`${getServerURL()}/mcp/${slug}`);
          if (!url) throw new Error(`Couldn’t open ${name} sign-in.`);
          setPhase("awaiting-signin");
          return url;
        } catch (err) {
          setPhase("idle");
          if (generation.current === run && status.data?.client)
            fail(
              "signin",
              `Couldn’t start ${name} sign-in. Try again.`,
              err instanceof ConfigurationChanged ? "settings" : undefined,
            );
          throw err;
        }
      },
      { name: `${providerKey}-setup-signin`, features: "" },
    );
    if (!opened) {
      setPhase("idle");
      fail("signin", `Allow pop-ups to sign in to ${name}.`);
    }
  };

  const test = async () => {
    const run = generation.current;
    if (pending) return;
    setPhase("testing");
    setFailure(null);
    setToolCount(null);
    dispatch({ type: "connection", tested: false });
    try {
      const result = await checkConnection();
      if (generation.current !== run) return;
      if (result.status === "ready" && result.toolCount != null) {
        setToolCount(result.toolCount);
        dispatch({ type: "connection", authorized: true, tested: true });
      } else {
        if (result.status === "authorization_required")
          dispatch({ type: "connection", authorized: false, tested: false });
        fail("test", connectionFailure(result, "test"), recoveryFor(result));
      }
    } catch {
      if (generation.current === run)
        fail(
          "test",
          `Couldn’t initialize ${name} and list its tools. Check app permissions, sign in again, and retry.`,
        );
    } finally {
      if (generation.current === run) setPhase("idle");
    }
  };

  const navigate: ProviderConnection["dispatch"] = (action) => {
    if (pending && action.type !== "secret") return;
    if (
      action.type === "complete" &&
      (status.isLoading || status.isError || failure)
    )
      return;
    if (action.type !== "secret") setFailure(null);
    dispatch(action);
  };
  const changeField: ProviderConnection["changeField"] = ({ field, value }) => {
    if (pending) return;
    setFailure(null);
    if (
      field === "access" &&
      value !== access &&
      status.data?.client &&
      status.data.serverCount > 1
    ) {
      fail(
        "save",
        "This app connection is shared. Update its permissions in Settings.",
        "settings",
      );
      return;
    }
    generation.current++;
    setToolCount(null);
    dispatch({ type: "field", field, value });
    if (field === "access" && value !== access && status.data?.client)
      void updatePermissions(scopesFor({ values: { access: value } }));
  };
  const next = async () => {
    if (pending || status.isLoading || status.isError) return;
    setFailure(null);
    if (
      status.data?.client &&
      !sameScopes(status.data.client.scope, requestedScopes)
    ) {
      if (!(await updatePermissions(requestedScopes))) return;
    }
    if (s.step === "credentials") await save();
    else dispatch({ type: "next" });
  };
  return {
    dispatch: navigate,
    next: () => void next(),
    changeField,
    signIn: () => void signIn(),
    testConnection: () => void test(),
    pending,
    error:
      failure?.message ??
      (status.error
        ? `Couldn’t load saved ${name} credentials. Try again.`
        : null),
    errorStep: failure?.step ?? "save",
    recovery: failure?.recovery,
    retryLoad: () => {
      void status.refetch().catch(() => {});
    },
    cancelSignIn: () => {
      generation.current++;
      cancelPopup();
      // An in-flight scope mutation must settle before another choice can write.
      setPhase(phase === "preparing-signin" ? "saving" : "idle");
      dispatch({ type: "connection", authorized: false, tested: false });
    },
    toolCount,
    loading: status.isLoading || status.isError,
    callbackUrl: status.data?.client.callbackUrl,
    savedClientId: status.data?.client.clientId,
  };
}
