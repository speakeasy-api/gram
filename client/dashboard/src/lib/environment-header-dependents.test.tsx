import { createQueryClient } from "@/contexts/Sdk";
import type { GetMcpServerEnvironmentHeadersRequest } from "@gram/client/models/operations/getmcpserverenvironmentheaders.js";
import { useCreateTunneledMcpServerHeaderMutation } from "@gram/client/react-query/createTunneledMcpServerHeader.js";
import { useDeleteRemoteMcpServerHeaderMutation } from "@gram/client/react-query/deleteRemoteMcpServerHeader.js";
import {
  queryKeyGetMcpServerEnvironmentHeaders,
  useGetMcpServerEnvironmentHeaders,
} from "@gram/client/react-query/getMcpServerEnvironmentHeaders.js";
import { mutationKeyCloneEnvironment } from "@gram/client/react-query/cloneEnvironment.js";
import { mutationKeyCreateRemoteMcpServerHeader } from "@gram/client/react-query/createRemoteMcpServerHeader.js";
import { mutationKeyDeleteEnvironment } from "@gram/client/react-query/deleteEnvironment.js";
import { mutationKeyDeleteTunneledMcpServerHeader } from "@gram/client/react-query/deleteTunneledMcpServerHeader.js";
import { mutationKeyUpdateMcpServer } from "@gram/client/react-query/updateMcpServer.js";
import { mutationKeyUpdateRemoteMcpServerHeader } from "@gram/client/react-query/updateRemoteMcpServerHeader.js";
import { mutationKeyUpdateTunneledMcpServerHeader } from "@gram/client/react-query/updateTunneledMcpServerHeader.js";
import {
  mutationKeyUpdateEnvironment,
  useUpdateEnvironmentMutation,
} from "@gram/client/react-query/updateEnvironment.js";
import {
  type MutationKey,
  type QueryClient,
  QueryClientProvider,
  useQuery,
} from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

// The app's query client and the generated hooks are real; only the SDK
// transport is replaced, so these tests prove what an environment or header
// write does to the cached tool listing and environment header preview.

const mocks = vi.hoisted(() => ({
  provenance: "mapped" as "mapped" | "overrides_source",
  tools: { prodOnlyTool: {} } as Record<string, unknown>,
}));

vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));

vi.mock("@gram/client/funcs/environmentsUpdateBySlug.js", () => ({
  environmentsUpdateBySlug: async () => ({ ok: true, value: {} }),
}));
vi.mock("@gram/client/funcs/tunneledMcpCreateServerHeader.js", () => ({
  tunneledMcpCreateServerHeader: async () => ({ ok: true, value: {} }),
}));
vi.mock("@gram/client/funcs/remoteMcpDeleteServerHeader.js", () => ({
  remoteMcpDeleteServerHeader: async () => ({ ok: true, value: undefined }),
}));
vi.mock("@gram/client/funcs/mcpServersGetEnvironmentHeaders.js", () => ({
  mcpServersGetEnvironmentHeaders: async (
    _client: unknown,
    request: GetMcpServerEnvironmentHeadersRequest,
  ) => ({
    ok: true,
    value: {
      environment: { id: request.environmentId, name: "Prod", slug: "prod" },
      environmentStatus: "ok",
      environmentConfigurationInvalid: false,
      environments: [],
      entries: [
        {
          entryName: "MCP_HEADER_X-Prod",
          headerName: "X-Prod",
          status: mocks.provenance,
        },
      ],
    },
  }),
}));

const toolsKey = [
  "proxiedMcpTools",
  "https://upstream.example.invalid/mcp/fixture",
  [],
];
const previewKey = queryKeyGetMcpServerEnvironmentHeaders({
  id: "server-1",
  selection: "environment",
  environmentId: "env-prod",
});

function seed(queryClient: QueryClient): void {
  queryClient.setQueryData(toolsKey, mocks.tools);
  queryClient.setQueryData(previewKey, { entries: [] });
}

function wrapper(queryClient: QueryClient) {
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
}

// useToolListing stands in for useProxiedMcpTools: the same key family and
// the same five-minute freshness, with a listing that tracks the upstream.
function useToolListing() {
  return useQuery({
    queryKey: toolsKey,
    queryFn: async () => mocks.tools,
    staleTime: 5 * 60 * 1000,
  });
}

function usePreview() {
  return useGetMcpServerEnvironmentHeaders({
    id: "server-1",
    selection: "environment",
    environmentId: "env-prod",
  });
}

afterEach(() => {
  mocks.provenance = "mapped";
  mocks.tools = { prodOnlyTool: {} };
});

describe("upstream header dependents", () => {
  const dependencies: [string, MutationKey][] = [
    ["environment update", mutationKeyUpdateEnvironment()],
    ["environment delete", mutationKeyDeleteEnvironment()],
    ["mcp server update", mutationKeyUpdateMcpServer()],
    ["remote header create", mutationKeyCreateRemoteMcpServerHeader()],
    ["remote header update", mutationKeyUpdateRemoteMcpServerHeader()],
    ["tunneled header update", mutationKeyUpdateTunneledMcpServerHeader()],
    ["tunneled header delete", mutationKeyDeleteTunneledMcpServerHeader()],
  ];
  it.each(dependencies)(
    "a successful %s invalidates both",
    async (_name, mutationKey) => {
      const queryClient = createQueryClient();
      seed(queryClient);
      await queryClient
        .getMutationCache()
        .build(queryClient, { mutationKey, mutationFn: async () => ({}) })
        .execute(undefined);

      await waitFor(() =>
        expect(queryClient.getQueryState(toolsKey)?.isInvalidated).toBe(true),
      );
      expect(queryClient.getQueryState(previewKey)?.isInvalidated).toBe(true);
    },
  );

  it("leaves both alone for an unrelated write", async () => {
    const queryClient = createQueryClient();
    seed(queryClient);
    await queryClient
      .getMutationCache()
      .build(queryClient, {
        mutationKey: mutationKeyCloneEnvironment(),
        mutationFn: async () => ({}),
      })
      .execute(undefined);

    expect(queryClient.getQueryState(toolsKey)?.isInvalidated).toBe(false);
    expect(queryClient.getQueryState(previewKey)?.isInvalidated).toBe(false);
  });

  it("refetches the tool listing after a linked environment's entry is edited", async () => {
    const queryClient = createQueryClient();
    const listing = renderHook(() => useToolListing(), {
      wrapper: wrapper(queryClient),
    });
    await waitFor(() =>
      expect(listing.result.current.data).toEqual({ prodOnlyTool: {} }),
    );

    const update = renderHook(() => useUpdateEnvironmentMutation(), {
      wrapper: wrapper(queryClient),
    });
    mocks.tools = { sandboxOnlyTool: {} };
    await act(async () => {
      await update.result.current.mutateAsync({
        request: {
          slug: "jamf-prod",
          updateEnvironmentRequestBody: {
            entriesToUpdate: [
              {
                name: "MCP_HEADER_X_INSTANCE_URL",
                value: "https://sandbox.example.invalid",
              },
            ],
            entriesToRemove: [],
          },
        },
      });
    });

    await waitFor(() =>
      expect(listing.result.current.data).toEqual({ sandboxOnlyTool: {} }),
    );
  });

  it("refreshes the preview's provenance after a source header is created", async () => {
    const queryClient = createQueryClient();
    const preview = renderHook(() => usePreview(), {
      wrapper: wrapper(queryClient),
    });
    await waitFor(() =>
      expect(preview.result.current.data?.entries[0]?.status).toBe("mapped"),
    );

    const create = renderHook(
      () => useCreateTunneledMcpServerHeaderMutation(),
      { wrapper: wrapper(queryClient) },
    );
    mocks.provenance = "overrides_source";
    await act(async () => {
      await create.result.current.mutateAsync({
        request: {
          createTunneledMcpServerHeaderForm: {
            tunneledMcpServerId: "tunnel-1",
            name: "X-Prod",
            isRequired: false,
            isSecret: false,
            value: "source-value",
          },
        },
      });
    });

    await waitFor(() =>
      expect(preview.result.current.data?.entries[0]?.status).toBe(
        "overrides_source",
      ),
    );
  });

  it("refreshes the preview's provenance after a source header is deleted", async () => {
    mocks.provenance = "overrides_source";
    const queryClient = createQueryClient();
    const preview = renderHook(() => usePreview(), {
      wrapper: wrapper(queryClient),
    });
    await waitFor(() =>
      expect(preview.result.current.data?.entries[0]?.status).toBe(
        "overrides_source",
      ),
    );

    const remove = renderHook(() => useDeleteRemoteMcpServerHeaderMutation(), {
      wrapper: wrapper(queryClient),
    });
    mocks.provenance = "mapped";
    await act(async () => {
      await remove.result.current.mutateAsync({ request: { id: "header-1" } });
    });

    await waitFor(() =>
      expect(preview.result.current.data?.entries[0]?.status).toBe("mapped"),
    );
  });
});
