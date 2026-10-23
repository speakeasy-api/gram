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
  items:
    [] as import("@gram/client/models/components/workloadtokenendpoint.js").WorkloadTokenEndpoint[],
}));
vi.mock("@gram/client/react-query/workloadTokenEndpoints.js", () => ({
  useWorkloadTokenEndpoints: () => ({
    data: { items: endpointQuery.items },
    isPending: false,
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
