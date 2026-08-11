import { getRBACScopeOverrideHeader } from "@/components/dev-toolbar-utils";
import { isProjectOverviewQueryKey } from "@/components/project/projectOverviewQuery";
import {
  capturePreservedStorage,
  clearStorageForLogout,
  restorePreservedStorage,
  type PreservedStorage,
} from "@/lib/logout-storage";
import { getApiBaseURL } from "@/lib/utils";
import { datadogRum } from "@datadog/browser-rum";
import { Gram } from "@gram/client";
import { HTTPClient } from "@gram/client/lib/http.js";
import { buildLatestDeploymentQuery } from "@gram/client/react-query/latestDeployment.core.js";
import { buildListToolsetsQuery } from "@gram/client/react-query/listToolsets.core.js";
import { GramProvider } from "@gram/client/react-query/_context.js";
import { QueryClientProvider } from "@tanstack/react-query";
import { useEffect, useMemo, useRef } from "react";
import { useTelemetry } from "./Telemetry";
import {
  IsPlatformAdminContext,
  SdkContext,
  queryClient,
  useProjectSlugForRequests,
  useSlugs,
} from "./Sdk";

const LOGOUT_PATH = "/rpc/auth.logout";

export const SdkProvider = ({
  children,
}: {
  children: React.ReactNode;
}): JSX.Element => {
  const projectSlug = useProjectSlugForRequests();
  const { projectSlug: pathProjectSlug } = useSlugs();
  const telemetry = useTelemetry();

  const isPlatformAdminRef = useRef(false);
  const previousProjectSlug = useRef(projectSlug);

  // Memoize the httpClient and gram instances
  const gram = useMemo(() => {
    // Values held across the logout round-trip. The logout response tells the
    // browser to wipe storage for the origin, which happens before the response
    // hook below runs, so anything meant to outlive the session has to be read
    // off localStorage before the request is sent.
    let preservedAcrossLogout: PreservedStorage = [];

    const httpClient = new HTTPClient({
      fetcher: (request) => {
        const newRequest = new Request(request, {
          credentials: "include",
        });

        if (projectSlug && !newRequest.headers.get("gram-project")) {
          newRequest.headers.set("gram-project", projectSlug);
        }

        const scopeOverride = getRBACScopeOverrideHeader(
          import.meta.env.DEV || isPlatformAdminRef.current,
        );
        if (scopeOverride) {
          newRequest.headers.set("X-Gram-Scope-Override", scopeOverride);
        }

        return fetch(newRequest);
      },
    });

    httpClient.addHook("beforeRequest", (request) => {
      if (new URL(request.url).pathname === LOGOUT_PATH) {
        preservedAcrossLogout = capturePreservedStorage();
      }
    });

    httpClient.addHook("response", (res, request) => {
      if (!res.ok) {
        return;
      }

      const u = new URL(request.url);
      if (u.pathname !== LOGOUT_PATH) {
        return;
      }

      datadogRum.stopSession();
      datadogRum.clearUser();
      telemetry.reset();
      document.cookie = "gram_admin_override=; path=/; max-age=0;";
      // Still clear explicitly: Clear-Site-Data is a no-op on origins the
      // browser does not treat as trustworthy, and on engines that don't
      // implement it. Where it did apply, this finds storage already empty and
      // the restore puts the preserved entries back either way.
      clearStorageForLogout();
      restorePreservedStorage(preservedAcrossLogout);
      preservedAcrossLogout = [];
    });

    const gram = new Gram({
      serverURL: getApiBaseURL(),
      httpClient,
    });

    // Prefetch key queries immediately so they run in parallel with auth.info
    // instead of waiting for auth to resolve before components mount and fire them.
    // Only prefetch when the user is actually on a project route — the
    // "default" fallback used for org-scoped pages shouldn't trigger work the
    // user will never see.
    if (pathProjectSlug) {
      void queryClient.prefetchQuery(buildLatestDeploymentQuery(gram));
      void queryClient.prefetchQuery(buildListToolsetsQuery(gram));
    }

    return gram;
    // eslint-disable-next-line react-hooks/exhaustive-deps -- telemetry is stable context value; including it would recreate the SDK client unnecessarily
  }, [projectSlug, pathProjectSlug]);

  // Invalidate all queries when projectSlug changes; most keys are not
  // project-aware. Overview keys are, and must survive so the org-home
  // prefetch isn't discarded on navigation.
  useEffect(() => {
    if (previousProjectSlug.current !== projectSlug) {
      void queryClient.invalidateQueries({
        predicate: (query) => !isProjectOverviewQueryKey(query.queryKey),
      });
      previousProjectSlug.current = projectSlug;
    }
  }, [projectSlug]);

  return (
    <IsPlatformAdminContext.Provider value={isPlatformAdminRef}>
      <QueryClientProvider client={queryClient}>
        <GramProvider client={gram}>
          <SdkContext.Provider value={gram}>{children}</SdkContext.Provider>
        </GramProvider>
      </QueryClientProvider>
    </IsPlatformAdminContext.Provider>
  );
};
