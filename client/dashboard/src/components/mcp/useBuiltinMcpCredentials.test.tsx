import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useBuiltinMcpCredentials } from "./useBuiltinMcpCredentials";
import type { BuiltinMcpServer } from "@gram/client/models/components/builtinmcpserver.js";

const state = {
  admin: true,
  environments: [
    {
      id: "env-default",
      name: "Default",
      slug: "default",
      entries: [{ name: "SLACK_BOT_TOKEN" }],
    },
    {
      id: "env-staging",
      name: "Staging",
      slug: "staging",
      entries: [{ name: "SLACK_USER_TOKEN" }],
    },
  ] as unknown[],
};

const listEnvironments = vi.fn();

vi.mock("@gram/client/react-query/listEnvironments.js", () => ({
  useListEnvironments: (
    request: unknown,
    security: unknown,
    options: { enabled?: boolean },
  ) => {
    listEnvironments(request, security, options);
    return {
      data: options.enabled ? { environments: state.environments } : undefined,
      isLoading: !options.enabled,
      isError: false,
    };
  },
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-one" }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => "project-one",
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: string, resourceId: string) =>
      state.admin && scope === "org:admin" && resourceId === "org-one",
    isLoading: false,
  }),
}));

const server = {
  key: "slack",
  environmentId: undefined,
  credentialVariables: ["SLACK_BOT_TOKEN", "SLACK_USER_TOKEN"],
} as unknown as BuiltinMcpServer;

beforeEach(() => {
  state.admin = true;
  listEnvironments.mockClear();
});

describe("useBuiltinMcpCredentials", () => {
  it("does not ask for environments without the admin scope the page requires", () => {
    state.admin = false;
    const { result } = renderHook(() => useBuiltinMcpCredentials(server));

    expect(listEnvironments).toHaveBeenCalledWith(
      expect.anything(),
      undefined,
      expect.objectContaining({ enabled: false }),
    );
    // Nothing was answered, so the absence of a credential is not a finding.
    expect(result.current.isLoading).toBe(true);
    expect(result.current.ready).toBe(false);
  });

  it("asks for environments once the scope is present", () => {
    const { result } = renderHook(() => useBuiltinMcpCredentials(server));

    expect(listEnvironments).toHaveBeenCalledWith(
      expect.anything(),
      undefined,
      expect.objectContaining({ enabled: true }),
    );
    expect(result.current.environment?.name).toBe("Default");
    expect(result.current.ready).toBe(true);
  });

  it("reads against a pending selection instead of the saved environment", () => {
    const { result } = renderHook(() =>
      useBuiltinMcpCredentials(server, "env-staging"),
    );

    expect(result.current.environment?.name).toBe("Staging");
    expect(
      result.current.credentials.find((c) => c.variable === "SLACK_USER_TOKEN")
        ?.set,
    ).toBe(true);
    expect(
      result.current.credentials.find((c) => c.variable === "SLACK_BOT_TOKEN")
        ?.set,
    ).toBe(false);
  });

  it("treats a pending project default as the default environment", () => {
    const saved = {
      ...server,
      environmentId: "env-staging",
    } as BuiltinMcpServer;
    const { result } = renderHook(() => useBuiltinMcpCredentials(saved, null));
    expect(result.current.environment?.name).toBe("Default");
  });
});
