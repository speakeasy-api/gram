import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { TooltipProvider } from "@/components/ui/Tooltip";
import type { DirectoryReviewItem } from "@gram/client/models/components/directoryreviewitem.js";
import type { Role } from "@gram/client/models/components/role.js";

import { DirectoryReviewPanel } from "./DirectoryReviewPanel";

const mocks = vi.hoisted(() => ({
  mutate: vi.fn(),
  refetch: vi.fn(),
  items: [] as DirectoryReviewItem[],
  failed: false,
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@gram/client/react-query/directoryReviewItems.js", () => ({
  useDirectoryReviewItems: () =>
    mocks.failed
      ? {
          data: undefined,
          isError: true,
          isFetching: false,
          refetch: mocks.refetch,
        }
      : {
          data: { items: mocks.items },
          isError: false,
          isFetching: false,
          refetch: mocks.refetch,
        },
}));
vi.mock("@gram/client/react-query/resolveDirectoryReviewItem.js", () => ({
  useResolveDirectoryReviewItemMutation: () => ({
    mutate: mocks.mutate,
    isPending: false,
  }),
}));
vi.mock("./invalidateDirectoryMappingAccess", () => ({
  invalidateDirectoryMappingAccess: vi.fn(),
}));

const roles = [
  {
    id: "role-support",
    name: "Support",
    principalUrn: "role:support",
    grants: [],
  },
] as unknown as Role[];

const newGroup: DirectoryReviewItem = {
  id: "item-group",
  kind: "new_group",
  occurrence: 1,
  createdAt: new Date("2026-10-01T00:00:00Z"),
  item: {
    source: { sourceKind: "group", directoryGroupId: "g1" },
    sourceName: "Platform",
    directoryPeople: 3,
    existingRoleUrns: [],
    proposedRoles: [
      { roleAction: "create", roleName: "Platform", currentGrants: [] },
    ],
    version: "v1",
  },
};

const newValue: DirectoryReviewItem = {
  id: "item-value",
  kind: "new_attribute_value",
  occurrence: 1,
  createdAt: new Date("2026-10-02T00:00:00Z"),
  item: {
    source: {
      sourceKind: "attribute",
      attributeKey: "department",
      attributeValue: "Sales",
    },
    sourceName: "department=Sales",
    directoryPeople: 1,
    existingRoleUrns: [],
    proposedRoles: [
      { roleAction: "create", roleName: "Sales", currentGrants: [] },
    ],
    version: "v2",
  },
};

const missingGroup: DirectoryReviewItem = {
  id: "item-missing",
  kind: "source_missing",
  occurrence: 2,
  createdAt: new Date("2026-10-03T00:00:00Z"),
  item: {
    source: { sourceKind: "group", directoryGroupId: "g2" },
    sourceName: "Contractors",
    directoryPeople: 0,
    existingRoleUrns: ["role:support"],
    proposedRoles: [],
    version: "v3",
  },
};

function renderPanel(rolesLoaded = true) {
  return render(
    <DirectoryReviewPanel
      roles={rolesLoaded ? roles : []}
      rolesLoaded={rolesLoaded}
    />,
    {
      wrapper: ({ children }) => (
        <QueryClientProvider client={new QueryClient()}>
          <TooltipProvider>{children}</TooltipProvider>
        </QueryClientProvider>
      ),
    },
  );
}

function requestOf(call: number) {
  return mocks.mutate.mock.calls[call]?.[0].request
    .resolveDirectoryReviewItemForm;
}

beforeEach(() => {
  mocks.items = [newGroup, newValue, missingGroup];
  mocks.failed = false;
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("DirectoryReviewPanel", () => {
  it("renders nothing when there is nothing to review", () => {
    mocks.items = [];
    const { container } = renderPanel();
    expect(container.innerHTML).toBe("");
  });

  it("lists each change with what deciding does", () => {
    renderPanel();
    expect(screen.getByText("3 directory changes to review")).toBeTruthy();
    expect(screen.getByText("New group: Platform")).toBeTruthy();
    expect(screen.getByText("New department value: Sales")).toBeTruthy();
    expect(screen.getByText("Group deleted: Contractors")).toBeTruthy();
    expect(
      screen.getByText(
        /Still mapped to Support, which grants nothing while it is gone. It has come back and gone again before/,
      ),
    ).toBeTruthy();
  });

  it("says a proposed new role starts with no access", () => {
    renderPanel();
    expect(
      screen.getAllByText(
        "A new role grants no access until you add permissions to it.",
      ),
    ).toHaveLength(2);
  });

  it("counts mapped roles until the role list loads", () => {
    renderPanel(false);
    expect(screen.getByText(/Still mapped to 1 role,/)).toBeTruthy();
    expect(screen.queryByText(/a deleted role/)).toBeNull();
  });

  it("shows a load failure with a retry", () => {
    mocks.failed = true;
    renderPanel();
    expect(
      screen.getByText("Couldn't load directory changes to review."),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(mocks.refetch).toHaveBeenCalled();
  });

  it("maps a new group to the proposed role", () => {
    renderPanel();
    fireEvent.click(screen.getAllByRole("button", { name: "Map" })[0]!);
    expect(requestOf(0)).toEqual({
      id: "item-group",
      decision: "approve",
      version: "v1",
      roles: [{ createName: "Platform" }],
    });
  });

  it("leaves a new source unmapped or stops asking about its key", () => {
    renderPanel();
    fireEvent.click(
      screen.getAllByRole("button", { name: "Leave unmapped" })[0]!,
    );
    expect(requestOf(0)).toEqual({ id: "item-group", decision: "dismiss" });
    fireEvent.click(
      screen.getByRole("button", { name: "Stop asking about department" }),
    );
    expect(requestOf(1)).toEqual({ id: "item-value", decision: "opt_out_key" });
  });

  it("removes or keeps a missing source's roles", () => {
    renderPanel();
    fireEvent.click(screen.getByRole("button", { name: "Remove roles" }));
    expect(requestOf(0)).toEqual({
      id: "item-missing",
      decision: "approve",
      version: "v3",
    });
    fireEvent.click(screen.getByRole("button", { name: "Keep roles" }));
    expect(requestOf(1)).toEqual({ id: "item-missing", decision: "dismiss" });
  });
});
