import {
  salesforceConnection,
  salesforceInstance,
  salesforceInstanceConfiguration,
  salesforceInstanceHeader,
  salesforceLogin,
  salesforceScopes,
  SalesforceInstanceConfigurationError,
  salesforceSelectionForUrl,
} from "@/pages/catalog/provider-setup/salesforce";
import { salesforceSourceFingerprint } from "./provider-setup";
import { invalidateRemoteMcpSourceViews } from "../tabs/settings/sections/sourceInvalidation";
import { invalidateAllRemoteMcpServerHeaders } from "@gram/client/react-query/remoteMcpServerHeaders";
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
  ready,
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
  github: {
    name: "GitHub",
    issuer: "https://github.com/login/oauth",
    authorizationEndpoint: "https://github.com/login/oauth/authorize",
    tokenEndpoint: "https://github.com/login/oauth/access_token",
  },
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
  savedTokenEndpointAuthMethod?: string;
  recovery?: "signin" | "credentials" | "settings" | "permissions";
  retryLoad: () => void;
  cancelSignIn: () => void;
};

function connectionFailure(
  result: { status: string; code: string },
  action: "signin" | "test",
  name: string,
): string {
  if (result.code === "scope_mismatch")
    return "Slack is still granting write access. Remove write scopes and revoke the app’s existing authorization in Slack, then sign in again.";
  if (result.code === "authorization_header_conflict")
    return "Remove the saved Authorization header in identity settings before testing User Identity.";
  if (result.status === "configuration_required")
    return "Review this server’s User Identity configuration before continuing.";
  if (result.status === "authorization_required")
    return `Sign in to ${name}, then test the connection again.`;
  return action === "signin"
    ? `Couldn’t verify ${name} sign-in. Try again.`
    : `Couldn’t test the ${name} connection. Review app permissions and try again.`;
}

// Shared setup actions use the management API; secrets and tokens are never progress state.
export function useProviderConnection(
  s: State,
  dispatch: Dispatch<Action>,
  server: McpServer,
  source?: { url: string; instanceUrl?: string },
): ProviderConnection {
  const providerKey = s.provider;
  if (providerKey === "salesforce" && source?.instanceUrl)
    source = {
      ...source,
      instanceUrl: salesforceInstance(source.instanceUrl) ?? source.instanceUrl,
    };
  const salesforce = useMemo(
    () =>
      providerKey === "salesforce" ? salesforceConnection(s.values) : null,
    [providerKey, s.values],
  );
  const config =
    providerKey === "salesforce"
      ? (salesforce ?? salesforceLogin())
      : providers[providerKey];
  const name = config.name;
  const access = s.values.access;
  const requestedScopes = useMemo(
    () =>
      providerKey === "slack"
        ? scopesFor({ values: { access } })
        : providerKey === "salesforce"
          ? salesforceScopes()
          : [],
    [access, providerKey],
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
  const activeSave = useRef<number | null>(null);
  const hydrated = useRef(false);
  const appliedSource = useRef(source ? { ...source } : undefined);
  const observedSource = useRef({ ...source, id: server.remoteMcpServerId });
  useEffect(() => {
    if (providerKey !== "salesforce" || !source) return;
    const previous = observedSource.current;
    const id = server.remoteMcpServerId;
    const changed =
      previous.id !== id ||
      previous.url !== source.url ||
      (previous.instanceUrl ?? "") !== (source.instanceUrl ?? "");
    if (changed) {
      observedSource.current = { ...source, id };
      const applied = appliedSource.current;
      // URL and header queries can refresh separately after our own writes.
      const ownRefresh =
        previous.id === id &&
        applied &&
        [previous.url, applied.url].includes(source.url) &&
        [previous.instanceUrl ?? "", applied.instanceUrl ?? ""].includes(
          source.instanceUrl ?? "",
        );
      if (!ownRefresh) {
        generation.current++;
        cancelPopup();
        if (activeSave.current === null) setPhase("idle");
        setToolCount(null);
        setFailure(null);
        appliedSource.current = { ...source };
        dispatch({
          type: "source",
          values: {
            ...salesforceSelectionForUrl(source.url),
            instanceUrl: source.instanceUrl ?? "",
          },
          sourceFingerprint: salesforceSourceFingerprint(id, source),
        });
        return;
      }
    }
    if (!s.sourceFingerprint && appliedSource.current)
      dispatch({
        type: "connection",
        sourceFingerprint: salesforceSourceFingerprint(
          id,
          appliedSource.current,
        ),
      });
  }, [
    providerKey,
    server.remoteMcpServerId,
    source,
    s.sourceFingerprint,
    dispatch,
    cancelPopup,
  ]);
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
    ...(providerKey === "salesforce" ? [config.issuer] : []),
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
    if (
      status.data &&
      (providerKey !== "salesforce" ||
        (!s.configChanged &&
          salesforce &&
          salesforce.url === appliedSource.current?.url &&
          (salesforce.instanceUrl ?? "") ===
            (appliedSource.current?.instanceUrl ?? "")))
    ) {
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
    salesforce,
    s.configChanged,
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

  const prepareSalesforceSource = useCallback(
    async (
      run: number,
      write: boolean,
      registerRollback?: (rollback: () => Promise<void>) => void,
    ) => {
      if (providerKey !== "salesforce") return;
      const selected = salesforceConnection(s.values);
      const id = server.remoteMcpServerId;
      if (!selected || !id || s.values.path !== "own")
        throw new ConfigurationChanged();
      const target = await sdk.mcpServers.get({ id: server.id });
      if (
        generation.current !== run ||
        target.remoteMcpServerId !== id ||
        target.environmentId !== server.environmentId ||
        target.userSessionIssuerId !== server.userSessionIssuerId
      )
        throw new ConfigurationChanged();
      const upstream = await sdk.remoteMcp.getServer({ id });
      if (generation.current !== run) throw new ConfigurationChanged();
      const { headers } = await sdk.remoteMcp.listServerHeaders({
        remoteMcpServerId: id,
      });
      if (generation.current !== run) throw new ConfigurationChanged();
      if (
        headers.some((header) => header.name.toLowerCase() === "authorization")
      )
        throw new ConfigurationChanged();
      const { header: instance, instanceUrl: currentInstance } =
        salesforceInstanceConfiguration(headers);
      const expected = appliedSource.current;
      if (
        !expected ||
        upstream.url !== expected.url ||
        currentInstance !== (expected.instanceUrl ?? "")
      )
        throw new ConfigurationChanged();
      const changeUrl = upstream.url !== selected.url;
      const changeInstance = currentInstance !== (selected.instanceUrl ?? "");
      if (!changeUrl && !changeInstance) return;
      if (!write) throw new ConfigurationChanged();
      const { mcpServers } = await sdk.mcpServers.list();
      if (generation.current !== run) throw new ConfigurationChanged();
      if (
        mcpServers.some(
          (item) => item.id !== server.id && item.remoteMcpServerId === id,
        )
      )
        throw new ConfigurationChanged();
      const originalSource = {
        url: upstream.url,
        instanceUrl: currentInstance,
      };
      registerRollback?.(async () => {
        if (generation.current !== run) throw new ConfigurationChanged();
        const latestTarget = await sdk.mcpServers.get({ id: server.id });
        const latestRemote = await sdk.remoteMcp.getServer({ id });
        const latestHeaders = await sdk.remoteMcp.listServerHeaders({
          remoteMcpServerId: id,
        });
        const latestInstance = salesforceInstanceConfiguration(
          latestHeaders.headers,
        );
        const latestLinks = await sdk.mcpServers.list();
        const applied = appliedSource.current;
        if (
          generation.current !== run ||
          latestLinks.mcpServers.some(
            (item) => item.id !== server.id && item.remoteMcpServerId === id,
          ) ||
          latestTarget.remoteMcpServerId !== id ||
          latestTarget.environmentId !== server.environmentId ||
          !applied ||
          latestRemote.url !== applied.url ||
          latestInstance.instanceUrl !== (applied.instanceUrl ?? "")
        )
          throw new ConfigurationChanged();
        if (latestRemote.url !== originalSource.url) {
          await sdk.remoteMcp.updateServer({
            updateServerForm: { id, url: originalSource.url },
          });
          appliedSource.current = {
            ...appliedSource.current!,
            url: originalSource.url,
          };
        }
        if (latestInstance.instanceUrl !== originalSource.instanceUrl) {
          if (instance) {
            const form = {
              name: instance.name,
              description: instance.description,
              value: instance.value,
              isRequired: instance.isRequired,
              isSecret: false,
            };
            if (latestInstance.header)
              await sdk.remoteMcp.updateServerHeader({
                updateServerHeaderForm: {
                  ...form,
                  id: latestInstance.header.id,
                },
              });
            else
              await sdk.remoteMcp.createServerHeader({
                createServerHeaderForm: { ...form, remoteMcpServerId: id },
              });
          } else if (latestInstance.header)
            await sdk.remoteMcp.deleteServerHeader({
              id: latestInstance.header.id,
            });
        }
        appliedSource.current = originalSource;
        dispatch({
          type: "connection",
          sourceFingerprint: salesforceSourceFingerprint(id, originalSource),
        });
      });
      if (changeInstance) {
        if (selected.instanceUrl) {
          const form = {
            name: instance?.name ?? salesforceInstanceHeader,
            description: instance?.description,
            value: selected.instanceUrl,
            isRequired: true,
            isSecret: false,
          };
          if (instance)
            await sdk.remoteMcp.updateServerHeader({
              updateServerHeaderForm: { ...form, id: instance.id },
            });
          else
            await sdk.remoteMcp.createServerHeader({
              createServerHeaderForm: { ...form, remoteMcpServerId: id },
            });
        } else if (instance)
          await sdk.remoteMcp.deleteServerHeader({ id: instance.id });
        if (generation.current !== run) throw new ConfigurationChanged();
        // Retain completed writes so a failed later operation can be retried.
        appliedSource.current = {
          ...expected,
          instanceUrl: selected.instanceUrl ?? "",
        };
        dispatch({
          type: "connection",
          sourceFingerprint: salesforceSourceFingerprint(
            id,
            appliedSource.current,
          ),
        });
      }
      if (generation.current !== run) throw new ConfigurationChanged();
      if (changeUrl) {
        await sdk.remoteMcp.updateServer({
          updateServerForm: { id, url: selected.url },
        });
        if (generation.current !== run) throw new ConfigurationChanged();
        appliedSource.current = {
          url: selected.url,
          instanceUrl: selected.instanceUrl ?? "",
        };
        dispatch({
          type: "connection",
          sourceFingerprint: salesforceSourceFingerprint(
            id,
            appliedSource.current,
          ),
        });
      }
      if (generation.current !== run) throw new ConfigurationChanged();
    },
    [
      providerKey,
      s.values,
      server.id,
      server.remoteMcpServerId,
      server.environmentId,
      server.userSessionIssuerId,
      sdk,
      dispatch,
    ],
  );

  const checkConnection = useCallback(async () => {
    const run = generation.current;
    await prepareSalesforceSource(run, false);
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
      providerKey === "slack" &&
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
    providerKey,
    status.data?.client.id,
    revokeSessions,
    prepareSalesforceSource,
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
          fail(
            "signin",
            connectionFailure(result, "signin", name),
            "permissions",
          );
        } else if (result.status !== "authorization_required") {
          setPhase("idle");
          fail(
            "signin",
            connectionFailure(result, "signin", name),
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
    if (activeSave.current !== null) return;
    const run = generation.current;
    if (s.saved && !s.values.clientId && !s.values.clientSecret) {
      dispatch({ type: "goto", step: "test" });
      return;
    }
    if (Object.keys(validate(s)).length) {
      dispatch({ type: "next" });
      return;
    }
    activeSave.current = run;
    setPhase("saving");
    setFailure(null);
    if (providerKey === "salesforce")
      dispatch({ type: "connection", configChanged: true });
    let rollbackSource: (() => Promise<void>) | undefined;
    let identityCommitted = false;
    try {
      await prepareSalesforceSource(run, true, (rollback) => {
        rollbackSource = rollback;
      });
      if (generation.current !== run) return;
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
        throw new ConfigurationChanged();
      }
      const issuerDigest =
        providerKey === "salesforce" && !provider
          ? Array.from(
              new Uint8Array(
                await crypto.subtle.digest(
                  "SHA-256",
                  new TextEncoder().encode(config.issuer),
                ),
              ),
            )
              .slice(0, 8)
              .map((byte) => byte.toString(16).padStart(2, "0"))
              .join("")
          : "";
      if (generation.current !== run) return;
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
                  slug: `${providerKey}-${server.id.slice(0, 8)}${issuerDigest ? `-${issuerDigest}` : ""}`,
                  name,
                  authorizationEndpoint: config.authorizationEndpoint,
                  tokenEndpoint: config.tokenEndpoint,
                  authorizationResponseIssParameterSupported:
                    providerKey === "github",
                  tokenEndpointAuthMethodsSupported:
                    providerKey === "salesforce"
                      ? ["none", "client_secret_post"]
                      : ["client_secret_post"],
                  responseTypesSupported: ["code"],
                  codeChallengeMethodsSupported: ["S256"],
                  grantTypesSupported: ["authorization_code", "refresh_token"],
                },
            clientMode: "manual",
            clientConfiguration: {
              clientId: s.values.clientId.trim(),
              clientSecret:
                providerKey === "salesforce"
                  ? undefined
                  : s.values.clientSecret.trim(),
              tokenEndpointAuthMethod:
                salesforce?.authMethod ?? "client_secret_post",
              scope: requestedScopes,
            },
          },
        },
      );
      if (result.status !== "registered" && result.status !== "linked")
        throw new Error(`Couldn’t save ${name} app credentials.`);
      identityCommitted = true;
      if (generation.current !== run) return;
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
        ...(providerKey === "salesforce" ? { configChanged: false } : {}),
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
      if (!identityCommitted && rollbackSource) {
        try {
          await rollbackSource();
        } catch {
          fail(
            "save",
            "Salesforce connection settings may be partially saved. Review the upstream URL and MCP-Instance-URL in identity settings before retrying credentials.",
            "settings",
          );
          return;
        }
      }
      if (err instanceof SalesforceInstanceConfigurationError) {
        fail("save", err.message, "settings");
        return;
      }
      const identityConflict =
        err instanceof ConfigurationChanged ||
        (err instanceof GramError && err.statusCode === 409);
      fail(
        "save",
        identityConflict
          ? "Review this server’s identity settings before saving these app credentials."
          : `Couldn’t save ${name} credentials. Check the app details and try again.`,
        identityConflict ? "settings" : undefined,
      );
    } finally {
      if (activeSave.current === run) {
        activeSave.current = null;
        setPhase("idle");
      }
      if (providerKey === "salesforce") {
        await Promise.all([
          invalidateRemoteMcpSourceViews(queryClient),
          invalidateAllRemoteMcpServerHeaders(queryClient),
        ]);
      }
    }
  };

  const signIn = async () => {
    const run = generation.current;
    if (pending || (providerKey === "salesforce" && !ready(s))) return;
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
      fail(
        "signin",
        `Use an HTTPS server address before signing in to ${name}.`,
      );
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
          await prepareSalesforceSource(run, false);
          if (providerKey === "slack") await syncPermissions(requestedScopes);
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
              err instanceof SalesforceInstanceConfigurationError
                ? err.message
                : `Couldn’t start ${name} sign-in. Try again.`,
              err instanceof ConfigurationChanged ||
                err instanceof SalesforceInstanceConfigurationError
                ? "settings"
                : undefined,
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
    if (pending || (providerKey === "salesforce" && !ready(s))) return;
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
        fail(
          "test",
          connectionFailure(result, "test", name),
          recoveryFor(result),
        );
      }
    } catch (err) {
      if (generation.current === run)
        fail(
          "test",
          err instanceof SalesforceInstanceConfigurationError
            ? err.message
            : `Couldn’t initialize ${name} and list its tools. Check app permissions, sign in again, and retry.`,
          err instanceof SalesforceInstanceConfigurationError
            ? "settings"
            : undefined,
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
      providerKey === "slack" &&
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
    if (
      providerKey === "slack" &&
      field === "access" &&
      value !== access &&
      status.data?.client
    )
      void updatePermissions(scopesFor({ values: { access: value } }));
  };
  const next = async () => {
    if (pending || status.isLoading || status.isError) return;
    setFailure(null);
    if (
      providerKey === "slack" &&
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
    savedTokenEndpointAuthMethod: status.data?.client.tokenEndpointAuthMethod,
  };
}
