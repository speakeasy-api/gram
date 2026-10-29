import { expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { groupTokenEndpoints, useSetupValues } from "./workloadTokenEndpoints";

function endpoint(
  id: string,
  slug: string,
  projectId = "",
  projectName = "",
): Parameters<typeof groupTokenEndpoints>[0][number] {
  return {
    userSessionIssuerId: id,
    userSessionIssuerSlug: slug,
    projectId,
    projectName,
    issuer: `https://gram.example.com/oauth/usi/${id}`,
    tokenEndpoint: `https://gram.example.com/oauth/usi/${id}/token`,
    mcpHost: "gram.example.com",
  };
}

it("groups issuers by owner, organization first, in the server's order", () => {
  const groups = groupTokenEndpoints([
    endpoint("o1", "org-issuer"),
    endpoint("p1", "alpha-issuer", "proj-a", "Alpha"),
    endpoint("p2", "beta-issuer", "proj-b", "Beta"),
    endpoint("p3", "alpha-second", "proj-a", "Alpha"),
  ]);
  expect(groups).toEqual([
    { label: "Organization", options: [{ id: "o1", label: "org-issuer" }] },
    {
      label: "Project: Alpha",
      options: [
        { id: "p1", label: "alpha-issuer" },
        { id: "p3", label: "alpha-second" },
      ],
    },
    { label: "Project: Beta", options: [{ id: "p2", label: "beta-issuer" }] },
  ]);
});

it("names an issuer by its id when it has no slug", () => {
  const groups = groupTokenEndpoints([endpoint("o1", "")]);
  expect(groups[0]?.options[0]?.label).toBe("o1");
});

it("keeps projects that share a name apart", () => {
  const groups = groupTokenEndpoints([
    endpoint("p1", "one", "proj-a", "Same"),
    endpoint("p2", "two", "proj-b", "Same"),
  ]);
  expect(groups.map((g) => g.label)).toEqual([
    "Project: Same (proj-a)",
    "Project: Same (proj-b)",
  ]);
});

const endpointQuery = vi.hoisted(() => ({
  pending: false,
  items:
    [] as import("@gram/client/models/components/workloadtokenendpoint.js").WorkloadTokenEndpoint[],
}));
vi.mock("@gram/client/react-query/workloadTokenEndpoints.js", () => ({
  useWorkloadTokenEndpoints: () => ({
    data: endpointQuery.pending ? undefined : { items: endpointQuery.items },
    isPending: endpointQuery.pending,
    isError: false,
    refetch: async () => ({ data: { items: endpointQuery.items } }),
  }),
}));
it("filters inference endpoints before selecting, including stale cross-project selection", () => {
  endpointQuery.items = [
    endpoint("b", "other-project", "proj-b"),
    endpoint("a", "current-project", "proj-a"),
    endpoint("org", "organization"),
  ];
  const { result, rerender } = renderHook(
    ({ projectId }) => useSetupValues(projectId),
    { initialProps: { projectId: "proj-a" } },
  );
  expect(result.current.selectedEndpointId).toBe("a");
  expect(
    result.current.endpointGroups.flatMap((group) =>
      group.options.map((option) => option.id),
    ),
  ).toEqual(["a", "org"]);
  act(() => {
    result.current.onEndpointChange("a");
  });
  rerender({ projectId: "proj-b" });
  expect(result.current.selectedEndpointId).toBe("b");
  expect(result.current.values?.token_endpoint).toBe(
    "https://gram.example.com/oauth/usi/b/token",
  );
});
it("treats only cross-project endpoints as empty for inference without a contact-sales dead end", () => {
  endpointQuery.items = [endpoint("b", "other-project", "proj-b")];
  const { result } = renderHook(() => useSetupValues("proj-a"));
  expect(result.current.values).toBeUndefined();
  expect(result.current.endpointGroups).toEqual([]);
  expect(result.current.unavailableReason).toBe(
    "No shared Speakeasy authorization server is available for this project or organization.",
  );
});

it("refreshes discovery and selects the newly created project issuer", async () => {
  endpointQuery.items = [endpoint("org", "organization")];
  const { result } = renderHook(() => useSetupValues("proj-a"));
  endpointQuery.items = [
    endpoint("org", "organization"),
    endpoint("created", "inference", "proj-a"),
  ];
  await act(async () => {
    await result.current.refreshAndSelect("created");
  });
  expect(result.current.selectedEndpointId).toBe("created");
  expect(result.current.values?.token_endpoint).toBe(
    "https://gram.example.com/oauth/usi/created/token",
  );
});
it("refuses to select a newly discovered issuer belonging to another project", async () => {
  endpointQuery.items = [
    endpoint("org", "organization"),
    endpoint("other", "inference", "proj-b"),
  ];
  const { result } = renderHook(() => useSetupValues("proj-a"));
  await expect(result.current.refreshAndSelect("other")).rejects.toThrow(
    "exchange endpoint is not available",
  );
  expect(result.current.selectedEndpointId).toBe("org");
});

function server(id: string, issuerId?: string) {
  return {
    id,
    name: `Server ${id}`,
    mcpUrl: `https://gram.example.com/mcp/${id}`,
    issuerId,
  };
}

it("preselects the issuer the agent's servers sign in through, not the organization's first", () => {
  endpointQuery.items = [
    endpoint("org", "organization"),
    endpoint("github", "github-issuer", "proj-a", "Alpha"),
    endpoint("unrelated", "unrelated", "proj-a", "Alpha"),
  ];
  const { result } = renderHook(() =>
    useSetupValues(undefined, [server("gh", "github")]),
  );
  expect(result.current.selectedEndpointId).toBe("github");
  expect(
    result.current.endpointGroups.flatMap((group) =>
      group.options.map((option) => option.id),
    ),
  ).toEqual(["github"]);
  expect(result.current.values?.token_endpoint).toBe(
    "https://gram.example.com/oauth/usi/github/token",
  );
  expect(result.current.uncoveredServers).toEqual([]);
});

it("preselects the issuer serving most of the agent's servers and reports the rest", () => {
  endpointQuery.items = [endpoint("one", "one"), endpoint("two", "two")];
  const servers = [
    server("a", "one"),
    server("b", "two"),
    server("c", "two"),
    server("d", "private"),
    server("e"),
  ];
  const { result } = renderHook(() => useSetupValues(undefined, servers));
  expect(result.current.selectedEndpointId).toBe("two");
  expect(result.current.coveredServers.map((s) => s.id)).toEqual([
    "b",
    "c",
    "e",
  ]);
  expect(
    result.current.uncoveredServers.map(({ server, reason }) => [
      server.id,
      reason,
    ]),
  ).toEqual([
    ["a", "another_issuer"],
    ["d", "not_shared"],
  ]);
});

it("moves coverage with the selected issuer", () => {
  endpointQuery.items = [endpoint("one", "one"), endpoint("two", "two")];
  const { result } = renderHook(() =>
    useSetupValues(undefined, [server("a", "one"), server("b", "two")]),
  );
  act(() => {
    result.current.onEndpointChange("two");
  });
  expect(result.current.coveredServers.map((s) => s.id)).toEqual(["b"]);
  expect(result.current.uncoveredServers[0]?.server.id).toBe("a");
});

it("explains when none of the agent's servers has a shared issuer instead of offering an unrelated one", () => {
  endpointQuery.items = [endpoint("org", "organization")];
  const { result } = renderHook(() =>
    useSetupValues(undefined, [server("gh", "private")]),
  );
  expect(result.current.values).toBeUndefined();
  expect(result.current.endpointGroups).toEqual([]);
  expect(result.current.unavailableReason).toMatch(
    /None of the servers below signs in through a user session issuer with a shared authorization server/,
  );
  expect(result.current.uncoveredServers).toEqual([
    {
      server: server("gh", "private"),
      reason: "not_shared",
      switchTo: { id: "org", label: "organization" },
    },
  ]);
});

it("offers every shared issuer when none of the agent's servers signs anyone in", () => {
  endpointQuery.items = [endpoint("org", "organization"), endpoint("p", "p")];
  const { result } = renderHook(() =>
    useSetupValues(undefined, [server("hosted")]),
  );
  expect(result.current.selectedEndpointId).toBe("org");
  expect(result.current.coveredServers.map((s) => s.id)).toEqual(["hosted"]);
  expect(result.current.uncoveredServers).toEqual([]);
});

it("leaves no server out while the issuers are still loading", () => {
  endpointQuery.items = [];
  endpointQuery.pending = true;
  try {
    const { result } = renderHook(() =>
      useSetupValues(undefined, [server("gh", "github")]),
    );
    expect(result.current.unavailableReason).toBe("Loading token endpoints…");
    expect(result.current.uncoveredServers).toEqual([]);
    expect(result.current.coveredServers.map((s) => s.id)).toEqual(["gh"]);
  } finally {
    endpointQuery.pending = false;
  }
});

it("offers to move a server without a shared issuer to the selected one", () => {
  endpointQuery.items = [endpoint("org", "organization"), endpoint("p", "p")];
  const { result } = renderHook(() =>
    useSetupValues(undefined, [server("a", "p"), server("b", "private")]),
  );
  expect(result.current.selectedEndpointId).toBe("p");
  expect(result.current.uncoveredServers[0]?.switchTo).toEqual({
    id: "p",
    label: "p",
  });
});

it("never offers to move a server to another project's issuer", () => {
  endpointQuery.items = [
    endpoint("other", "other-project", "proj-b"),
    endpoint("own", "own-project", "proj-a"),
  ];
  const lonely = { ...server("gh", "private"), projectId: "proj-a" };
  const { result } = renderHook(() => useSetupValues(undefined, [lonely]));
  expect(result.current.uncoveredServers[0]?.switchTo).toEqual({
    id: "own",
    label: "own-project",
  });

  endpointQuery.items = [endpoint("other", "other-project", "proj-b")];
  const { result: none } = renderHook(() =>
    useSetupValues(undefined, [lonely]),
  );
  expect(none.current.uncoveredServers[0]?.switchTo).toBeUndefined();
});
