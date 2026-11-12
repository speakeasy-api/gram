import { useUserSessionToken } from "@/hooks/useUserSessionToken";
import { useResolvedMcpServerUrl } from "@/hooks/useToolsetUrl";
import { mcpConnectionUrl } from "@/lib/utils";
import type { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import type { MetaMcpServer } from "@gram/client/models/components/metamcpserver.js";
import { useQueries, useQuery } from "@tanstack/react-query";
import { useEffect, useMemo, useRef } from "react";
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
    isError: query.isError,
    refetch: () => void refetch(),
  };
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
  | { kind: "unavailable"; why: string; retry?: () => void };

/** The words both Overview and Inspect use for a member's state. */
export const MEMBER_STATE_LABEL: Record<MemberViewerState["kind"], string> = {
  checking: "Checking…",
  available: "Available",
  sign_in: "Sign in needed",
  unavailable: "Unavailable",
};

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
    target: { kind: "metaMcpServer", id: live ? metaMcpServer.id : undefined },
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

  const slugOf = (row: MemberRow) =>
    row.server?.slug ?? row.member.mcpServerSlug;
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
          retry: token.error ? token.refetch : signIn.refetch,
        };
      }
      const status = signIn.statusFor(slugOf(row));
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
        return {
          kind: "unavailable",
          why: status.detail || "This server can't be used right now.",
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
          retry: () => void check.refetch(),
        };
      }
      return { kind: "available" };
    },
  };
}
