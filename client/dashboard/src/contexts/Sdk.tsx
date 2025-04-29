import { getServerURL } from "@/lib/utils";
import { Gram } from "@gram/client";
import { HTTPClient } from "@gram/client/lib/http.js";
import { GramProvider } from "@gram/client/react-query/index.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createContext, useContext } from "react";

export const SdkContext = createContext<Gram>({} as Gram);

export const useSdkClient = () => {
  return useContext(SdkContext);
};

export const SdkProvider = ({ children }: { children: React.ReactNode }) => {
  // Temporary measure to allow the dashboard to persist cookies between calls to the server
  // If the dashboard eventually keeps track of sesssion and manually attaches or we move to a BFF model we can remove this
  const httpClient = new HTTPClient({
    fetcher: (request) => {
      const newRequest = new Request(request, {
        credentials: "include",
      });

      return fetch(newRequest);
    },
  });

  const gram = new Gram({
    serverURL: getServerURL(),
    httpClient,
  });

  const queryClient = new QueryClient();

  return (
    <QueryClientProvider client={queryClient}>
      <GramProvider client={gram}>
        <SdkContext.Provider value={gram}>{children}</SdkContext.Provider>
      </GramProvider>
    </QueryClientProvider>
  );
};
