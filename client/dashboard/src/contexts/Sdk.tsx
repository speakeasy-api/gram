import { handleError } from "@/lib/errors";
import {
  isGramSessionUnauthorizedError,
  isUnauthorizedError,
} from "@/lib/route-errors";
import { redirectToLoginOnUnauthorized } from "@/lib/session-expired";
import { Gram } from "@gram/client";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { QueryCache, QueryClient } from "@tanstack/react-query";
import { createContext, useContext } from "react";
import { useLocation, useParams } from "react-router";

// SdkProvider cannot call useIsPlatformAdmin() directly because it wraps AuthProvider
// (AuthProvider needs QueryClientProvider which SdkProvider supplies). Instead,
// SdkProvider owns a ref and exposes it via context so AuthHandler can write isPlatformAdmin
// into it, making the current value available to the fetcher on every request.
export const IsPlatformAdminContext = createContext<
  React.MutableRefObject<boolean>
>({
  current: false,
});
export const useIsPlatformAdminRef = (): React.MutableRefObject<boolean> =>
  useContext(IsPlatformAdminContext);

export const SdkContext = createContext<Gram>({} as Gram);

export const useSdkClient = (): Gram => {
  const client = useContext(SdkContext);
  return client;
};

// Preserve QueryClient across HMR to prevent cache loss
const createQueryClient = () =>
  new QueryClient({
    // A Gram API 401 means the session is dead (expired, revoked, or — in
    // local dev — overwritten by another worktree's stack, since the cookie is
    // scoped to `localhost` and cookies ignore ports). Send the user to /login
    // instead of letting it throw to the error boundary. Only Gram errors
    // qualify: queries that talk to non-Gram endpoints (e.g. useProxiedMcpTools
    // listing a proxied MCP server's tools) 401 when the *upstream* wants
    // credentials, which their hooks handle inline — redirecting on those loops
    // the page through /login forever since the Gram session is still valid.
    queryCache: new QueryCache({
      onError: (error) => {
        if (isGramSessionUnauthorizedError(error)) {
          redirectToLoginOnUnauthorized();
        }
      },
    }),
    defaultOptions: {
      queries: {
        // Suppress 403s so RBAC-restricted queries degrade gracefully
        // instead of crashing the page, and 401s because they are auth
        // states, not crashes: Gram 401s are already navigating away via the
        // cache handler above, and non-Gram 401s (e.g. a proxied MCP upstream
        // wanting credentials) are surfaced inline by their hooks. All other
        // errors still throw to the nearest error boundary.
        throwOnError: (error) =>
          !isUnauthorizedError(error) &&
          !(error instanceof GramError && error.statusCode === 403),
        retry: (failureCount, error: Error) => {
          // Don't retry on 4xx errors
          if (error && typeof error === "object") {
            let status = (error as unknown as { status: unknown }).status;
            if (typeof status !== "number") {
              status = (error as unknown as { statusCode: unknown }).statusCode;
            }

            if (typeof status === "number" && status >= 400 && status < 500) {
              return false;
            }
          }
          // Default retry logic for other errors
          return failureCount < 3;
        },
      },
      mutations: {
        onError: (error: Error) => {
          handleError(error, { title: "Request failed" });
        },
      },
    },
  });

// In development, preserve queryClient across HMR
export const queryClient: QueryClient =
  (import.meta.hot?.data?.queryClient as QueryClient) ?? createQueryClient();

// Vite always provides `hot.data`, but vitest's shim defines `hot` without it.
if (import.meta.hot?.data) {
  import.meta.hot.data.queryClient = queryClient;
}

export const useSlugs = (): {
  orgSlug: string | undefined;
  projectSlug: string | undefined;
} => {
  let { orgSlug, projectSlug } = useParams();
  const location = useLocation();

  // If we don't have params from React Router, extract from pathname.
  // Project routes live at /:orgSlug/projects/:projectSlug/...
  if (!orgSlug) {
    const parts = location.pathname.split("/").filter(Boolean);
    orgSlug = parts[0];
    if (parts[1] === "projects" && parts.length >= 3) {
      projectSlug = parts[2];
    }
  }

  return { orgSlug, projectSlug };
};

/**
 * Resolves the project slug to send on the `gram-project` request header. On
 * project-scoped routes this matches the path slug. On org-scoped routes that
 * still call project-scoped endpoints (e.g. the onboarding wizard hitting
 * plugins.getPublishStatus), it honors an explicit `?projectSlug=` query
 * param, then falls back to the `default` project every org has.
 *
 * Use this only for SDK header injection — UI redirect logic and link targets
 * should keep using `useSlugs()` so they can still distinguish "user is on an
 * org-only page" from "user is on a project page".
 */
export const useProjectSlugForRequests = (): string => {
  const { projectSlug } = useSlugs();
  const location = useLocation();
  if (projectSlug) return projectSlug;
  // Treat empty string the same as missing — `?projectSlug=` yields `""`
  // from URLSearchParams.get, which would suppress the header otherwise.
  const search = new URLSearchParams(location.search);
  const queryProjectSlug = search.get("projectSlug");
  if (queryProjectSlug) return queryProjectSlug;
  return "default";
};
