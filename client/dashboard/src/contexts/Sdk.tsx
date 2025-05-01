import { getServerURL } from "@/lib/utils";
import { Gram } from "@gram/client";
import { HTTPClient } from "@gram/client/lib/http.js";
import { GramProvider } from "@gram/client/react-query/index.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createContext, useContext, useMemo, useEffect, useRef } from "react";
import { useLocation, useParams } from "react-router-dom";

export const SdkContext = createContext<Gram>({} as Gram);

export const useSdkClient = () => {
  return useContext(SdkContext);
};

export const SdkProvider = ({ children }: { children: React.ReactNode }) => {
  const { projectSlug } = useSlugs();
  const queryClient = useMemo(() => new QueryClient(), []);
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
