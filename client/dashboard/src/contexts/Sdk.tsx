import { handleError } from "@/lib/errors";
import { Gram } from "@gram/client";
import { QueryClient } from "@tanstack/react-query";
import { createContext, useContext } from "react";
import { useLocation, useParams } from "react-router";

// SdkProvider cannot call useIsAdmin() directly because it wraps AuthProvider
// (AuthProvider needs QueryClientProvider which SdkProvider supplies). Instead,
// SdkProvider owns a ref and exposes it via context so AuthHandler can write isAdmin
// into it, making the current value available to the fetcher on every request.
export const IsAdminContext = createContext<React.MutableRefObject<boolean>>({
  current: false,
});
export const useIsAdminRef = () => useContext(IsAdminContext);

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
export const queryClient: QueryClient =
  (import.meta.hot?.data?.queryClient as QueryClient) ?? createQueryClient();

if (import.meta.hot) {
  import.meta.hot.data.queryClient = queryClient;
}

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
