import type { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Toolset } from "@/lib/toolTypes";
import { HostedAddressSections } from "./HostedAddressSections";

const testState = vi.hoisted(() => ({
  mutate: vi.fn(),
  mutationOptions: undefined as
    | {
        onSuccess?: (endpoint: unknown) => void;
        onError?: (error: Error) => void;
      }
    | undefined,
  domain: undefined as
    | { id: string; domain: string; activated: boolean; verified: boolean }
    | undefined,
  takenOnDomain: undefined as string | undefined,
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
  invalidateMcpServerQueries: vi.fn(),
  patchMcpEndpointInCache: vi.fn(),
}));

vi.mock("@gram/client/react-query/updateMcpEndpoint.js", () => ({
  useUpdateMcpEndpointMutation: (options: typeof testState.mutationOptions) => {
    testState.mutationOptions = options;
    return { mutate: testState.mutate, isPending: false };
  },
}));
vi.mock("@/pages/mcp/mcp-details-utils", () => ({
  useMcpSlugValidation: (
    slug: string | undefined,
    _current: string | undefined,
    customDomainId: string | undefined,
  ) =>
    slug && customDomainId && customDomainId === testState.takenOnDomain
      ? "This slug is already taken"
      : null,
}));
vi.mock("@/lib/mcp-server-visibility", () => ({
  invalidateMcpServerQueries: testState.invalidateMcpServerQueries,
}));
vi.mock("@/hooks/useRootMcpEndpoint", () => ({
  patchMcpEndpointInCache: testState.patchMcpEndpointInCache,
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: () => true,
    hasAnyScope: () => true,
    hasAllScopes: () => true,
    isLoading: false,
  }),
}));
vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ capture: vi.fn() }),
}));
vi.mock("@/hooks/useProductTier", () => ({
  useProductTier: () => "pro",
}));
vi.mock("@/hooks/useToolsetUrl", () => ({
  useCustomDomain: () => ({ domain: testState.domain }),
  useMcpUrl: () => ({ customServerURL: undefined }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({ settings: { goTo: vi.fn() } }),
}));
vi.mock("react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("react-router")>()),
  useParams: () => ({ orgSlug: "acme" }),
}));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({}),
}));
vi.mock("@/components/ui/Tooltip", () => ({
  Tooltip: ({ children }: { children: ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ children }: { children: ReactNode }) => <>{children}</>,
  TooltipContent: ({ children }: { children: ReactNode }) => (
    <span>{children}</span>
  ),
}));
vi.mock("@/components/FeatureRequestModal", () => ({
  FeatureRequestModal: () => null,
}));
vi.mock("sonner", () => ({
  toast: { success: testState.toastSuccess, error: testState.toastError },
}));

const platformToolset = {
  id: "toolset-1",
  slug: "hosted",
  mcpSlug: "acme-hosted",
} as Toolset;

const customDomainToolset = {
  ...platformToolset,
  mcpSlug: "hosted",
  customDomainId: "domain-1",
} as Toolset;

function endpoint(overrides: Partial<McpEndpoint> = {}): McpEndpoint {
  return {
    id: "endpoint-1",
    projectId: "project-1",
    mcpServerId: "toolset-1",
    slug: "acme-hosted",
    isDomainRoot: false,
    createdAt: new Date(0),
    updatedAt: new Date(0),
    ...overrides,
  };
}

function renderSections(
  toolset: Toolset,
  endpoints: McpEndpoint[],
  isLoadingEndpoints = false,
) {
  return render(
    <HostedAddressSections
      toolset={toolset}
      endpoints={endpoints}
      isLoadingEndpoints={isLoadingEndpoints}
    />,
  );
}

function editSlug(value: string) {
  fireEvent.change(screen.getByPlaceholderText("my-server"), {
    target: { value },
  });
}

beforeEach(() => {
  testState.mutate.mockReset();
  testState.mutationOptions = undefined;
  testState.domain = undefined;
  testState.takenOnDomain = undefined;
  testState.toastSuccess.mockClear();
  testState.toastError.mockClear();
  testState.invalidateMcpServerQueries.mockClear();
  testState.patchMcpEndpointInCache.mockClear();
});

afterEach(cleanup);

describe("HostedAddressSections", () => {
  it("saves a platform slug through mcpEndpoints.update", () => {
    renderSections(platformToolset, [endpoint()]);

    editSlug("acme-renamed");
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(testState.mutate).toHaveBeenCalledWith({
      request: {
        updateMcpEndpointForm: {
          id: "endpoint-1",
          mcpServerId: "toolset-1",
          slug: "acme-renamed",
          customDomainId: undefined,
        },
      },
    });
  });

  it("keeps the custom domain when saving a slug on it", () => {
    renderSections(customDomainToolset, [
      endpoint({ slug: "hosted", customDomainId: "domain-1" }),
    ]);

    editSlug("renamed");
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(testState.mutate).toHaveBeenCalledWith({
      request: {
        updateMcpEndpointForm: {
          id: "endpoint-1",
          mcpServerId: "toolset-1",
          slug: "renamed",
          customDomainId: "domain-1",
        },
      },
    });
  });

  it("links the custom domain with the current slug", () => {
    testState.domain = {
      id: "domain-2",
      domain: "mcp.example.com",
      activated: true,
      verified: true,
    };
    renderSections(platformToolset, [endpoint()]);

    fireEvent.click(screen.getByRole("button", { name: "Link Domain" }));

    expect(testState.mutate).toHaveBeenCalledWith({
      request: {
        updateMcpEndpointForm: {
          id: "endpoint-1",
          mcpServerId: "toolset-1",
          slug: "acme-hosted",
          customDomainId: "domain-2",
        },
      },
    });
  });

  it("blocks linking when the slug is taken on the target domain", () => {
    testState.domain = {
      id: "domain-2",
      domain: "mcp.example.com",
      activated: true,
      verified: true,
    };
    testState.takenOnDomain = "domain-2";
    renderSections(platformToolset, [endpoint()]);

    const link = screen.getByRole("button", { name: "Link Domain" });
    expect(link).toHaveProperty("disabled", true);
    expect(screen.getByText("This slug is already taken")).toBeTruthy();
  });

  it("falls back to the server's single non-root endpoint", () => {
    renderSections(platformToolset, [
      endpoint({ id: "endpoint-2", slug: "acme-drifted" }),
    ]);

    editSlug("acme-renamed");
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(testState.mutate).toHaveBeenCalledWith({
      request: {
        updateMcpEndpointForm: expect.objectContaining({ id: "endpoint-2" }),
      },
    });
  });

  it("explains when no endpoint can be edited", () => {
    renderSections(platformToolset, [
      endpoint({ id: "endpoint-2", slug: "acme-a" }),
      endpoint({ id: "endpoint-3", slug: "acme-b" }),
    ]);

    editSlug("acme-renamed");
    expect(screen.getByRole("button", { name: "Save" })).toHaveProperty(
      "disabled",
      true,
    );
    expect(screen.getByText(/address could not be loaded/)).toBeTruthy();
  });

  it("shows no hint while endpoints load", () => {
    renderSections(platformToolset, [], true);
    expect(screen.queryByText(/address could not be loaded/)).toBeNull();
  });

  it("invalidates server queries and toasts on success", () => {
    renderSections(platformToolset, [endpoint()]);
    const updated = endpoint({ slug: "acme-renamed" });

    testState.mutationOptions?.onSuccess?.(updated);

    expect(testState.patchMcpEndpointInCache).toHaveBeenCalledWith(
      expect.anything(),
      updated,
    );
    expect(testState.invalidateMcpServerQueries).toHaveBeenCalled();
    expect(testState.toastSuccess).toHaveBeenCalledWith(
      "MCP settings saved successfully",
    );
  });

  it("shows the server error and discards the staged slug on failure", () => {
    renderSections(platformToolset, [endpoint()]);
    editSlug("acme-renamed");

    act(() => {
      testState.mutationOptions?.onError?.(new Error("slug is reserved"));
    });

    expect(testState.toastError).toHaveBeenCalledWith("slug is reserved");
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
  });
});
