import { getServerURL } from "@/lib/utils";
import { datadogRum } from "@datadog/browser-rum";
import { Gram } from "@gram/client";
import { HTTPClient } from "@gram/client/lib/http.js";
import { GramProvider } from "@gram/client/react-query/index.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createContext, useContext, useEffect, useMemo, useRef } from "react";
import { useLocation, useParams } from "react-router";
import { useTelemetry } from "./Telemetry";
import { handleError } from "@/lib/errors";
import { initializeToastClient } from "@/lib/toast";

export const SdkContext = createContext<Gram>({} as Gram);

export const useSdkClient = () => {
  const client = useContext(SdkContext);
  return client;
};

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      throwOnError: true,
      retry: (failureCount, error: Error) => {
        // Don't retry on 4xx errors
        if (error && typeof error === "object" && "status" in error) {
          const status = (error as unknown as { status: number }).status;
          if (status >= 400 && status < 500) {
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

        if (projectSlug) {
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

    return gram;
  }, [projectSlug]);

  // Invalidate all queries when projectSlug changes
  useEffect(() => {
    if (previousProjectSlug.current !== projectSlug) {
      queryClient.invalidateQueries();
      previousProjectSlug.current = projectSlug;
    }
  }, [projectSlug, queryClient]);

  // Initialize toast client for notification persistence
  useEffect(() => {
    initializeToastClient(gram);
  }, [gram]);

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

  // If we don't have params, extract from pathname
  if (!orgSlug || !projectSlug) {
    const parts = location.pathname.split("/").filter(Boolean);
    if (parts.length >= 2) {
      orgSlug = parts[0];
      projectSlug = parts[1];
    }
  }

  return { orgSlug, projectSlug };
};
