import { useUserSessionToken } from "@/hooks/useUserSessionToken";
import { useResolvedMcpServerUrl } from "@/hooks/useToolsetUrl";
import { mcpConnectionUrl } from "@/lib/utils";
import type { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import type { MetaMcpServer } from "@gram/client/models/components/metamcpserver.js";
import {
  type QueryClient,
  useQueries,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState } from "react";
import { z } from "zod";
import type { MemberRow } from "./memberRows";
import { authCacheKey, gatewayRPC } from "./useGatewayInspection";

/** Sign-in states where opening the gateway's connect page helps. */
const NEEDS_SIGN_IN = new Set(["not_connected", "needs_consent"]);
/** Sign-in states where the member's tools should load for the viewer. */
const READY = new Set(["connected", "not_required", "verified_at_consent"]);

const connectionStatusSchema = z.object({
  structuredContent: z
    .object({
      connectUrl: z.string().optional(),
      audience: z.string().optional(),
      servers: z.array(
        z.object({
          slug: z.string(),
          status: z.string(),
          detail: z.string().optional(),
        }),
      ),
    })
    .optional(),
});

type MemberSignInStatus = { status: string; detail?: string };

export interface GatewayMemberSignIn {
  /** True when this member needs the viewer's own account and has none. */
  needsSignIn: (memberSlug: string | undefined) => boolean;
  /** The gateway's sign-in status for this member, once read. */
  statusFor: (memberSlug: string | undefined) => MemberSignInStatus | undefined;
  /** Opens the gateway's connect page; coming back checks again. */
  connect: () => void;
  /** True when there is a page the viewer can open to connect. */
  canConnect: boolean;
  isLoading: boolean;
  /** A read is in flight, including a refresh of data already shown. */
  isFetching: boolean;
  /** The status has been read at least once. */
  hasStatus: boolean;
  isError: boolean;
  refetch: () => void;
}

/**
 * Which gateway members need the viewer to connect an account, read from the
 * gateway's own sign-in status the way an agent would. Only the viewer can
 * connect, so an agent-audience result offers nothing. When the viewer comes
 * back from the connect page, the status is read again and `onConnected`
 * runs, so the caller can list the members' tools again.
 */
export function useGatewayMemberSignIn(
  connectUrl: string | undefined,
  headers: Record<string, string> | undefined,
  options: { enabled: boolean; onConnected?: () => void },
): GatewayMemberSignIn {
  const enabled = options.enabled && !!connectUrl;
  const query = useQuery({
    queryKey: ["gatewayConnectionStatus", connectUrl, authCacheKey(headers)],
    enabled,
    retry: false,
    throwOnError: false,
    staleTime: 60 * 1000,
    queryFn: async () => {
      if (!connectUrl) throw new Error("No gateway URL configured");
      const result = connectionStatusSchema.parse(
        await gatewayRPC(connectUrl, headers, "tools/call", {
          name: "connection_status",
          arguments: {},
        }),
      );
      return result.structuredContent;
    },
  });

  const status = query.data;
  const pageUrl =
    status?.connectUrl && !status.audience ? status.connectUrl : undefined;
  const bySlug = useMemo(
    () =>
      new Map<string, MemberSignInStatus>(
        (status?.servers ?? []).map((server) => [server.slug, server]),
      ),
    [status],
  );
  const pending = useMemo(
    () =>
      new Set(
        pageUrl
          ? [...bySlug]
              .filter(([, server]) => NEEDS_SIGN_IN.has(server.status))
              .map(([slug]) => slug)
          : [],
      ),
    [pageUrl, bySlug],
  );

  // Coming back from the connect tab: check again, then refresh the caller.
  const onConnected = useRef(options.onConnected);
  onConnected.current = options.onConnected;
  const { refetch } = query;
  useEffect(() => {
    if (pending.size === 0) return;
    const onFocus = () => {
      void refetch().then(() => onConnected.current?.());
    };
    window.addEventListener("focus", onFocus);
    return () => window.removeEventListener("focus", onFocus);
  }, [pending, refetch]);

  return {
    needsSignIn: (slug) => !!slug && pending.has(slug),
    statusFor: (slug) => (slug ? bySlug.get(slug) : undefined),
    connect: () => {
      if (pageUrl) window.open(pageUrl, "_blank", "noopener,noreferrer");
    },
    canConnect: !!pageUrl,
    isLoading: enabled && query.isLoading,
    isFetching: enabled && query.isFetching,
    hasStatus: !!status,
    isError: query.isError,
    refetch: () => void refetch(),
  };
}

/**
 * Reads every member's state again, the way a new connection would. The
 * gateway fixes which upstream connection a session uses when the session is
 * minted, so a settings change only shows once the dashboard mints again: the
 * session token goes too, not just the status call.
 */
function refreshGatewayMemberStates(
  queryClient: QueryClient,
): Promise<unknown> {
  return Promise.all([
    queryClient.invalidateQueries({
      queryKey: ["userSessionToken", "metaMcpServer"],
    }),
    queryClient.invalidateQueries({ queryKey: ["gatewayConnectionStatus"] }),
    queryClient.invalidateQueries({ queryKey: ["gatewayMemberCheck"] }),
  ]);
}

/**
 * The SDK resources whose writes change what a member gives the viewer:
 * sign-in setup, credentials, members and their servers. Running a tool or
 * sending a chat writes nothing here, and must not reconnect Inspect.
 */
const CONFIG_RESOURCES =
  /^(remoteSessionClients|remoteSessionIssuers|userSessionIssuers|metaMcp|mcpServers|remoteMcp|toolsets|environments|mcpMetadata)/i;

/**
 * A configuration change saved anywhere in the app can fix or break a member: a linked
 * sign-in, a shared credential, a member added. So every successful configuration write
 * reads the states again, and the header and rows never show a fixed issue.
 */
function useRefreshOnSavedChanges(enabled: boolean): void {
  const queryClient = useQueryClient();
  useEffect(() => {
    if (!enabled) return;
    return queryClient.getMutationCache().subscribe((event) => {
      if (event.type !== "updated" || event.action.type !== "success") return;
      const [source, resource] = (event.mutation.options.mutationKey ??
        []) as unknown[];
      if (source !== "@gram/client" || typeof resource !== "string") return;
      if (!CONFIG_RESOURCES.test(resource)) return;
      void refreshGatewayMemberStates(queryClient);
    });
  }, [enabled, queryClient]);
}

export interface GatewayMemberStates {
  stateFor: (row: MemberRow) => MemberViewerState | undefined;
  /** Opens the gateway's connect page for the viewer. */
  connect: () => void;
  signIn: GatewayMemberSignIn;
}

/** What a gateway member gives the viewer right now. */
export type MemberViewerState =
  | { kind: "checking" }
  | { kind: "available" }
  | { kind: "sign_in" }
  | {
      kind: "unavailable";
      why: string;
      /**
       * `why` is the server's own text, fit for a tooltip but not for the
       * main message.
       */
      fromServer?: boolean;
      /** The server needs a shared credential set on its own settings. */
      needsCredential?: boolean;
      /**
       * The server signs people in through an authorization server this
       * gateway has no link to, which an admin adds on the gateway's own
       * sign-in settings. Retrying changes nothing until then.
       */
      needsGatewaySignIn?: boolean;
      /** The check itself failed, so nothing is known about the server. */
      checkFailed?: boolean;
      retry?: () => void;
    };

/** The words both Overview and Inspect use for a member's state. */
export const MEMBER_STATE_LABEL: Record<MemberViewerState["kind"], string> = {
  checking: "Checking…",
  available: "Available",
  sign_in: "Sign in needed",
  unavailable: "Unavailable",
};

/**
 * The gateway's wording when a member has no credential it can use for this
 * caller, which an admin fixes on the member's own sign-in settings. Matched
 * on text because the error carries no code.
 */
export const NEEDS_CREDENTIAL =
  /requires authentication and has no credential|rejected the stored credential|rejected the credential configured|upstream credential that is misconfigured or was rejected/i;

/**
 * The gateway's wording when a member's sign-in runs through an
 * authorization server the gateway isn't linked to, so no connection can be
 * chosen for it. Its connect page says the same: "isn't set up on this
 * gateway yet".
 */
export const NEEDS_GATEWAY_SIGN_IN =
  /signed in to again to choose its connection/i;

/** Plain words for a member whose sign-in isn't set up on the gateway. */
export const GATEWAY_SIGN_IN_WHY =
  "Its sign-in isn't set up on this gateway. An admin adds its authorization server in the gateway's sign-in settings.";

const describedServerSchema = z.object({
  isError: z.boolean().optional(),
  content: z.array(z.object({ text: z.string().optional() })).optional(),
});

/**
 * Each member's state for the signed-in viewer, for a page without a gateway
 * session of its own (Overview). One sign-in status call covers every member;
 * a remote member that reports ready is then asked for its tool names once,
 * so a server that is down reads as Unavailable rather than Available.
 * Excluded members (disabled, unproxied, no slug, unresolved) get no state.
 *
 * Overview, Inspect and the page header share it: the session and the status
 * call use the same query keys on each, so one status call serves them all.
 * With `checkTools: "cache"` (the header) the tool-name check is only read
 * from what another page already loaded, never fetched.
 */
export function useGatewayMemberStates(
  metaMcpServer: MetaMcpServer | undefined,
  endpoints: McpEndpoint[],
  isLoadingEndpoints: boolean,
  rows: MemberRow[],
  options: { checkTools?: "fetch" | "cache"; onConnected?: () => void } = {},
): GatewayMemberStates {
  const { mcpUrl, loading } = useResolvedMcpServerUrl(
    endpoints,
    isLoadingEndpoints,
  );
  const connectUrl = useMemo(() => mcpConnectionUrl(mcpUrl), [mcpUrl]);
  const hasIssuer = !!metaMcpServer?.userSessionIssuerId;
  const live = !!metaMcpServer && metaMcpServer.visibility !== "disabled";
  const token = useUserSessionToken({
    // Direct and Code Mode serve no status tools, so the check always runs
    // in Progressive, whatever mode the gateway is saved in.
    target: {
      kind: "metaMcpServer",
      id: live ? metaMcpServer.id : undefined,
      discoveryMode: "progressive",
      connectionVersion: "member-status",
    },
    userSessionIssuerId: metaMcpServer?.userSessionIssuerId,
    throwOnError: false,
  });
  const headers = useMemo(
    () =>
      token.accessToken
        ? { Authorization: `Bearer ${token.accessToken}` }
        : undefined,
    [token.accessToken],
  );
  const sessionReady = live && !loading && (!hasIssuer || !!headers);
  const signIn = useGatewayMemberSignIn(connectUrl, headers, {
    enabled: sessionReady,
    onConnected: options.onConnected,
  });
  const fetchChecks = (options.checkTools ?? "fetch") === "fetch";
  // Every caller subscribes; react-query folds the repeated invalidations.
  useRefreshOnSavedChanges(live);

  const slugOf = (row: MemberRow) =>
    row.server?.slug ?? row.member.mcpServerSlug;

  // A member added after the status was read is missing from it. Read it
  // again once per member set, and call such members checking meanwhile, so
  // a new member never reads as one the gateway doesn't serve.
  const memberKey = rows
    .flatMap((row) => slugOf(row) ?? [])
    .sort((a, b) => a.localeCompare(b))
    .join(",");
  const missingFromStatus =
    signIn.hasStatus &&
    rows.some(
      (row) =>
        (row.classification === "hosted" || row.classification === "proxied") &&
        !signIn.statusFor(slugOf(row)),
    );
  const [refetchedFor, setRefetchedFor] = useState<string | null>(null);
  const awaitingRefetch = missingFromStatus && refetchedFor !== memberKey;
  const refetchSignIn = signIn.refetch;
  const statusFetching = signIn.isFetching;
  useEffect(() => {
    if (!awaitingRefetch || !sessionReady || statusFetching) return;
    setRefetchedFor(memberKey);
    refetchSignIn();
  }, [awaitingRefetch, sessionReady, statusFetching, memberKey, refetchSignIn]);
  const checked = rows.filter(
    (row) =>
      row.classification === "proxied" &&
      READY.has(signIn.statusFor(slugOf(row))?.status ?? ""),
  );
  const checks = useQueries({
    queries: checked.map((row) => ({
      queryKey: [
        "gatewayMemberCheck",
        connectUrl,
        slugOf(row),
        authCacheKey(headers),
      ],
      enabled: fetchChecks && sessionReady && !!connectUrl,
      retry: false,
      throwOnError: false,
      staleTime: 5 * 60 * 1000,
      queryFn: async () => {
        if (!connectUrl) throw new Error("No gateway URL configured");
        const described = describedServerSchema.parse(
          await gatewayRPC(connectUrl, headers, "tools/call", {
            name: "describe_server",
            arguments: { server: slugOf(row) },
          }),
        );
        if (described.isError) {
          throw new Error(
            described.content?.[0]?.text ??
              "The gateway couldn't read this server's tools.",
          );
        }
        return true;
      },
    })),
  });
  const checkBySlug = new Map(
    checked.map((row, index) => [slugOf(row), checks[index]]),
  );

  return {
    connect: signIn.connect,
    signIn,
    stateFor: (row) => {
      if (row.classification !== "hosted" && row.classification !== "proxied")
        return undefined;
      if (!live) return undefined;
      if (!sessionReady || signIn.isLoading || token.isLoading)
        return { kind: "checking" };
      if (signIn.isError || token.error) {
        return {
          kind: "unavailable",
          why: "Couldn't check this server for you.",
          checkFailed: true,
          retry: token.error ? token.refetch : signIn.refetch,
        };
      }
      const status = signIn.statusFor(slugOf(row));
      // A member added since the status was read waits for the new read.
      if (!status && (signIn.isFetching || awaitingRefetch))
        return { kind: "checking" };
      if (!status) {
        // Not served to this viewer, e.g. a private server they can't use.
        return {
          kind: "unavailable",
          why: "This gateway doesn't serve this server to you.",
        };
      }
      if (NEEDS_SIGN_IN.has(status.status)) {
        return signIn.canConnect
          ? { kind: "sign_in" }
          : {
              kind: "unavailable",
              why: "This server needs each person's own account, but this gateway doesn't ask anyone to sign in.",
            };
      }
      if (!READY.has(status.status)) {
        if (NEEDS_GATEWAY_SIGN_IN.test(status.detail ?? ""))
          return {
            kind: "unavailable",
            why: GATEWAY_SIGN_IN_WHY,
            needsGatewaySignIn: true,
          };
        return {
          kind: "unavailable",
          why: status.detail || "This server can't be used right now.",
          fromServer: !!status.detail,
          needsCredential: NEEDS_CREDENTIAL.test(status.detail ?? ""),
          retry: signIn.refetch,
        };
      }
      if (row.classification === "hosted") return { kind: "available" };
      const check = checkBySlug.get(slugOf(row));
      // Cache-only callers trust the sign-in status when nothing is cached.
      if (!fetchChecks && (!check || check.status === "pending"))
        return { kind: "available" };
      if (!check || check.isLoading) return { kind: "checking" };
      if (check.isError) {
        return {
          kind: "unavailable",
          why: check.error.message,
          fromServer: true,
          needsCredential: NEEDS_CREDENTIAL.test(check.error.message),
          retry: () => void check.refetch(),
        };
      }
      return { kind: "available" };
    },
  };
}
