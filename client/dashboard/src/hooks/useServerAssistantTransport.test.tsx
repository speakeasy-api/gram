import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useServerAssistantTransport } from "./useServerAssistantTransport";

const mocks = vi.hoisted(() => ({
  grants: new Set<string>(),
  getManaged: vi.fn(),
  ensureManaged: vi.fn(),
}));

vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));

vi.mock("@gram/client/react-query/assistantsGetManaged.js", () => ({
  useAssistantsGetManaged: (...args: unknown[]) => mocks.getManaged(...args),
}));

vi.mock("@gram/client/react-query/ensureManagedAssistant.js", () => ({
  useEnsureManagedAssistantMutation: () => ({ mutate: mocks.ensureManaged }),
}));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({
    projects: [{ id: "project_a", slug: "project-a" }],
  }),
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: string, resourceId?: string) =>
      resourceId === "project_a" && mocks.grants.has(scope),
    isLoading: false,
  }),
}));

vi.mock("@/lib/route-errors", () => ({
  isNotFoundError: (error: unknown) =>
    (error as { code?: string } | null)?.code === "not_found",
}));

vi.mock("@/lib/ServerAssistantTransport", () => ({
  createServerAssistantTransport: vi.fn(() => vi.fn()),
}));

beforeEach(() => {
  mocks.grants.clear();
  mocks.getManaged.mockReset();
  mocks.ensureManaged.mockReset();
  mocks.getManaged.mockReturnValue({ data: undefined, error: undefined });
});

describe("useServerAssistantTransport", () => {
  it("does not enable assistant reads when access is denied", () => {
    const { result } = renderHook(() =>
      useServerAssistantTransport("project-a", true),
    );

    expect(mocks.getManaged.mock.calls[0]?.[2]).toMatchObject({
      enabled: false,
    });
    expect(mocks.ensureManaged).not.toHaveBeenCalled();
    expect(result.current.allowed).toBe(false);
    expect(result.current.ready).toBe(false);
  });

  it("lets read-only callers use an existing assistant without provisioning", () => {
    mocks.grants.add("assistant:read");
    mocks.getManaged.mockReturnValue({
      data: { id: "assistant_a" },
      error: undefined,
    });

    const { result } = renderHook(() =>
      useServerAssistantTransport("project-a", true),
    );

    expect(mocks.getManaged.mock.calls[0]?.[2]).toMatchObject({
      enabled: true,
    });
    expect(mocks.ensureManaged).not.toHaveBeenCalled();
    expect(result.current.allowed).toBe(true);
    expect(result.current.ready).toBe(true);
  });

  it("only provisions a missing assistant for writers", async () => {
    mocks.grants.add("assistant:read");
    mocks.getManaged.mockReturnValue({
      data: undefined,
      error: { code: "not_found" },
    });

    const readOnly = renderHook(() =>
      useServerAssistantTransport("project-a", true),
    );
    expect(readOnly.result.current.needsAdmin).toBe(true);
    expect(mocks.ensureManaged).not.toHaveBeenCalled();
    readOnly.unmount();

    mocks.grants.add("assistant:write");
    renderHook(() => useServerAssistantTransport("project-a", true));

    await waitFor(() => {
      expect(mocks.ensureManaged).toHaveBeenCalledWith(
        { request: { gramProject: "project-a" } },
        expect.any(Object),
      );
    });
  });
});
