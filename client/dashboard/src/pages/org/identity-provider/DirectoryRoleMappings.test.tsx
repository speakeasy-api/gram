import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

import type { DirectoryRoleMapping } from "@gram/client/models/components/directoryrolemapping.js";
import { DirectoryRoleMappings } from "./DirectoryRoleMappings";
import { MemoryRouter } from "react-router";
import type { Role } from "@gram/client/models/components/role.js";
import { TooltipProvider } from "@/components/ui/Tooltip";

const mocks = vi.hoisted(() => ({
  save: vi.fn(),
  mappings: [] as DirectoryRoleMapping[],
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({ createRole: { href: () => "/roles/new" } }),
}));
vi.mock("@gram/client/react-query/directoryRoleMappings.js", () => ({
  useDirectoryRoleMappings: () => ({
    data: {
      groups: [{ id: "group-1", name: "Engineering", memberCount: 2 }],
      attributes: [],
      mappings: mocks.mappings,
    },
    isPending: false,
  }),
  invalidateAllDirectoryRoleMappings: vi.fn(),
}));
vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: () => ({ data: { members: [] } }),
}));
vi.mock("@gram/client/react-query/roles.js", () => ({
  useRoles: () => ({ data: { roles } }),
}));
vi.mock("@gram/client/react-query/setDirectoryRoleMappings.js", () => ({
  useSetDirectoryRoleMappingsMutation: () => ({
    mutate: mocks.save,
    isPending: false,
  }),
  mutationKeySetDirectoryRoleMappings: () => ["set-mappings"],
}));
vi.mock("@gram/client/react-query/syncDirectoryGroups.js", () => ({
  useSyncDirectoryGroupsMutation: () => ({ mutate: vi.fn(), isPending: false }),
}));
vi.mock("./invalidateDirectoryMappingAccess", () => ({
  invalidateDirectoryMappingAccess: vi.fn(),
}));
vi.mock("@/components/ui/Combobox", () => ({
  Combobox: ({
    items,
    onSelectionChange,
  }: {
    items: { value: string; label: string }[];
    onSelectionChange: (item: { value: string; label: string }) => void;
  }) => (
    <div>
      {items.map((item) => (
        <button key={item.value} onClick={() => onSelectionChange(item)}>
          Add {item.label}
        </button>
      ))}
    </div>
  ),
}));

const roles: Role[] = ["Base", "Tools", "Support"].map((name) => ({
  id: name,
  name,
  description: "",
  grants: [],
  isSystem: false,
  memberCount: 0,
  principalUrn: `role:organization:${name}`,
  slug: name.toLowerCase(),
  createdAt: new Date(),
  updatedAt: new Date(),
}));
function mapping(name: string): DirectoryRoleMapping {
  return {
    id: name,
    roleUrn: `role:organization:${name}`,
    sourceKind: "group",
    directoryGroupId: "group-1",
    createdAt: new Date(),
    updatedAt: new Date(),
  };
}
function renderMappings(): void {
  render(
    <MemoryRouter>
      <QueryClientProvider client={new QueryClient()}>
        <TooltipProvider>
          <DirectoryRoleMappings />
        </TooltipProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  mocks.save.mockReset();
  mocks.mappings = [mapping("Base"), mapping("Tools")];
});
afterEach(cleanup);

describe("directory source role sets", () => {
  it("shows both roles and only offers unmapped roles to add", () => {
    renderMappings();
    expect(screen.getByText("Base")).toBeDefined();
    expect(screen.getByText("Tools")).toBeDefined();
    expect(screen.queryByRole("button", { name: "Add Base" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Add Tools" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Add Support" }));
    expect(mocks.save).toHaveBeenCalledWith({
      request: {
        setDirectoryRoleMappingsForm: {
          sourceKind: "group",
          directoryGroupId: "group-1",
          roleUrns: roles.map((role) => role.principalUrn),
        },
      },
    });
  });

  it("removes one role without removing the others", () => {
    renderMappings();
    fireEvent.click(
      screen.getByRole("button", { name: "Remove Base from Engineering" }),
    );
    expect(mocks.save).toHaveBeenCalledWith({
      request: {
        setDirectoryRoleMappingsForm: {
          sourceKind: "group",
          directoryGroupId: "group-1",
          roleUrns: ["role:organization:Tools"],
        },
      },
    });
  });

  it("removes the final role with an empty set", () => {
    mocks.mappings = [mapping("Base")];
    renderMappings();
    fireEvent.click(
      screen.getByRole("button", { name: "Remove Base from Engineering" }),
    );
    expect(mocks.save).toHaveBeenCalledWith({
      request: {
        setDirectoryRoleMappingsForm: {
          sourceKind: "group",
          directoryGroupId: "group-1",
          roleUrns: [],
        },
      },
    });
  });
});
