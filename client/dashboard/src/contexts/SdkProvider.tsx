import { getRBACScopeOverrideHeader } from "@/components/dev-toolbar-utils";
import { getServerURL } from "@/lib/utils";
import { datadogRum } from "@datadog/browser-rum";
import { Gram } from "@gram/client";
import { HTTPClient } from "@gram/client/lib/http.js";
import { buildLatestDeploymentQuery } from "@gram/client/react-query/latestDeployment.core.js";
import { buildListToolsetsQuery } from "@gram/client/react-query/listToolsets.core.js";
import { GramProvider } from "@gram/client/react-query/index.js";
import { QueryClientProvider } from "@tanstack/react-query";
import { useEffect, useMemo, useRef } from "react";
import { useTelemetry } from "./Telemetry";
import { IsAdminContext, SdkContext, queryClient, useSlugs } from "./Sdk";

export const SdkProvider = ({ children }: { children: React.ReactNode }) => {
  const { projectSlug } = useSlugs();
  const telemetry = useTelemetry();

  const isAdminRef = useRef(false);
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

        const scopeOverride = getRBACScopeOverrideHeader(
          import.meta.env.DEV || isAdminRef.current,
        );
        if (scopeOverride) {
          newRequest.headers.set("X-Gram-Scope-Override", scopeOverride);
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
    // eslint-disable-next-line react-hooks/exhaustive-deps -- telemetry is stable context value; including it would recreate the SDK client unnecessarily
  }, [projectSlug]);

  // Invalidate all queries when projectSlug changes
  useEffect(() => {
    if (previousProjectSlug.current !== projectSlug) {
      queryClient.invalidateQueries();
      previousProjectSlug.current = projectSlug;
    }
  }, [projectSlug]);

  return (
    <IsAdminContext.Provider value={isAdminRef}>
      <QueryClientProvider client={queryClient}>
        <GramProvider client={gram}>
          <SdkContext.Provider value={gram}>{children}</SdkContext.Provider>
        </GramProvider>
      </QueryClientProvider>
    </IsAdminContext.Provider>
  );
};
