import { cleanup, renderHook, waitFor, act } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { GramCore } from "@gram/client/core.js";
import { HTTPClient } from "@gram/client/lib/http.js";
import { afterEach, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { useGatewayConnectionCandidates } from "./useGatewayConnectionCandidates";

const state = vi.hoisted(() => ({
  sdk: undefined as unknown,
  navigate: vi.fn(),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-one" }),
  useProject: () => ({ id: "project-one" }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => state.sdk,
  useProjectSlugForRequests: () => "project-one",
  useSlugs: () => ({ orgSlug: "acme", projectSlug: "route-project" }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => true }),
}));
vi.mock("react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("react-router")>()),
  useLocation: () => ({ pathname: "/", search: "" }),
  useNavigate: () => state.navigate,
}));
vi.mock("@gram/client/react-query/metaMcpServers.js", () => ({
  buildMetaMcpServersQuery: () => ({
    queryKey: ["gateways"],
    queryFn: async () => ({
      metaMcpServers: [
        {
          id: "gateway-one",
          name: "Example gateway",
          userSessionIssuerId: "issuer-one",
        },
      ],
    }),
  }),
}));
afterEach(cleanup);

it("requests the SDK next cursor before exposing a later-page registration", async () => {
  const requests: URL[] = [];
  const release = Promise.withResolvers<void>();
  state.sdk = new GramCore({
    serverURL: "https://api.example.test",
    retryConfig: { strategy: "none" },
    httpClient: new HTTPClient({
      fetcher: async (request) => {
        const req = request as Request;
        const url = new URL(req.url);
        requests.push(url);
        expect(req.headers.get("Gram-Project")).toBe("project-one");
        expect(url.searchParams.get("limit")).toBe("100");
        const cursor = url.searchParams.get("cursor");
        if (cursor) {
          expect(cursor).toBe("page-two");
          await release.promise;
        }
        const id = cursor ? "later-page" : "unrelated";
        return new Response(
          JSON.stringify({
            items: [
              {
                id,
                client_id: id,
                client_name: id,
                user_session_issuer_id: cursor ? "issuer-one" : "issuer-two",
                active_session_count: 0,
                credential_kind: "public",
                redirect_uris: [],
                client_id_issued_at: "2026-01-01T00:00:00Z",
                created_at: "2026-01-01T00:00:00Z",
                updated_at: "2026-01-01T00:00:00Z",
              },
            ],
            ...(cursor ? {} : { next_cursor: "page-two" }),
          }),
          { headers: { "Content-Type": "application/json" } },
        );
      },
    }),
  });
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const { result, unmount } = renderHook(
    () => useGatewayConnectionCandidates({ enabled: true, inProject: true }),
    {
      wrapper: ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={client}>{children}</QueryClientProvider>
      ),
    },
  );
  try {
    await waitFor(() => expect(requests).toHaveLength(2));
    expect(requests[0]!.searchParams.has("cursor")).toBe(false);
    expect(requests[1]!.searchParams.get("cursor")).toBe("page-two");
    expect(
      result.current.some((candidate) => candidate.title === "later-page"),
    ).toBe(false);
    await act(async () => {
      release.resolve();
      await release.promise;
    });
    await waitFor(() =>
      expect(
        result.current.some((candidate) => candidate.title === "later-page"),
      ).toBe(true),
    );
    const registration = result.current.find(
      (candidate) => candidate.title === "later-page",
    )!;
    await registration.run("open");
    expect(state.navigate).toHaveBeenCalledWith(
      "/acme/projects/project-one/mcp/gateway/gateway-one/sessions?registration=later-page",
    );
    expect(requests).toHaveLength(2);
  } finally {
    release.resolve();
    unmount();
    client.clear();
  }
});
