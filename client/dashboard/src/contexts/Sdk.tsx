import { handleError } from "@/lib/errors";
import { getServerURL } from "@/lib/utils";
import { datadogRum } from "@datadog/browser-rum";
import { Gram } from "@gram/client";
import { HTTPClient } from "@gram/client/lib/http.js";
import { buildLatestDeploymentQuery } from "@gram/client/react-query/latestDeployment.core.js";
import { buildListToolsetsQuery } from "@gram/client/react-query/listToolsets.core.js";
import { GramProvider } from "@gram/client/react-query/index.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createContext, useContext, useEffect, useMemo, useRef } from "react";
import { useLocation, useParams } from "react-router";
import { useTelemetry } from "./Telemetry";

export const SdkContext = createContext<Gram>({} as Gram);

export const useSdkClient = () => {
  const client = useContext(SdkContext);
  return client;
};

// Preserve QueryClient across HMR to prevent cache loss
const createQueryClient = () =>
  new QueryClient({
    defaultOptions: {
      queries: {
        throwOnError: true,
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
const queryClient: QueryClient =
  (import.meta.hot?.data?.queryClient as QueryClient) ?? createQueryClient();

if (import.meta.hot) {
  import.meta.hot.data.queryClient = queryClient;
}

// Set staleTime defaults so prefetched data isn't immediately refetched when hooks mount.
// These match the staleTime values used by the consuming hooks.
const ONE_HOUR = 1000 * 60 * 60;
queryClient.setQueryDefaults(["@gram/client", "deployments", "latest"], {
  staleTime: ONE_HOUR,
});
queryClient.setQueryDefaults(["@gram/client", "toolsets", "list"], {
  staleTime: ONE_HOUR,
});

export const SdkProvider = ({ children }: { children: React.ReactNode }) => {
  const { projectSlug } = useSlugs();
  const telemetry = useTelemetry();

  const previousProjectSlug = useRef(projectSlug);

  // Memoize the httpClient and gram instances
  const gram = useMemo(() => {
    const httpClient = new HTTPClient({
      fetcher: (request) => {
        const newRequest = new Request(request, {
          credentials: "include",
        });

        if (projectSlug && !newRequest.headers.get("gram-project")) {
          newRequest.headers.set("gram-project", projectSlug);
        }

        return fetch(newRequest);
      },
    });

    httpClient.addHook("response", (res, request) => {
      if (!res.ok) {
        return;
      }

      const u = new URL(request.url);
      if (u.pathname !== "/rpc/auth.logout") {
        return;
      }

      datadogRum.stopSession();
      datadogRum.clearUser();
      telemetry.reset();
      if (typeof localStorage !== "undefined") {
        localStorage.clear();
      }
      if (typeof sessionStorage !== "undefined") {
        sessionStorage?.clear();
      }
    });

    const gram = new Gram({
      serverURL: getServerURL(),
      httpClient,
    });

    // Prefetch key queries immediately so they run in parallel with auth.info
    // instead of waiting for auth to resolve before components mount and fire them.
    if (projectSlug) {
      queryClient.prefetchQuery(buildLatestDeploymentQuery(gram));
      queryClient.prefetchQuery(buildListToolsetsQuery(gram));
    }

    return gram;
  }, [projectSlug]);

  // Invalidate all queries when projectSlug changes
  useEffect(() => {
    if (previousProjectSlug.current !== projectSlug) {
      queryClient.invalidateQueries();
      previousProjectSlug.current = projectSlug;
    }
  }, [projectSlug, queryClient]);

  return (
    <QueryClientProvider client={queryClient}>
      <GramProvider client={gram}>
        <SdkContext.Provider value={gram}>{children}</SdkContext.Provider>
      </GramProvider>
    </QueryClientProvider>
  );
};

export const useSlugs = () => {
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
