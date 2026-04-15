import { GramProvider } from "@gram/client/react-query";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createDashboardGramClient } from "./lib/gram";

export function createTestQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
}

export function TestQueryWrapper({
  children,
  queryClient,
}: {
  children: React.ReactNode;
  queryClient: QueryClient;
}) {
  const gram = createDashboardGramClient();

  return (
    <QueryClientProvider client={queryClient}>
      <GramProvider client={gram}>{children}</GramProvider>
    </QueryClientProvider>
  );
}

/**
 * Extract the URL from a fetch call argument (handles both string and Request).
 */
export function extractFetchUrl(input: unknown): string {
  return typeof input === "string" ? input : (input as Request).url;
}
