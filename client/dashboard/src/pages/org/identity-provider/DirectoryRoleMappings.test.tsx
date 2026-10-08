import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import {
  completeCreateRoleFlow,
  startCreateRoleFlow,
} from "./directoryMappingFlow";

import type { DirectoryRoleMapping } from "@gram/client/models/components/directoryrolemapping.js";
import { DirectoryRoleMappings } from "./DirectoryRoleMappings";
import { MemoryRouter } from "react-router";
import type { Role } from "@gram/client/models/components/role.js";
import { TooltipProvider } from "@/components/ui/Tooltip";

const mocks = vi.hoisted(() => ({
  save: vi.fn(),
  refetch: vi.fn(),
  toastError: vi.fn(),
  fetchedAfterMount: true,
  mappings: [] as DirectoryRoleMapping[],
}));
vi.mock("sonner", () => ({
  toast: { error: mocks.toastError, success: vi.fn() },
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
    isFetchedAfterMount: mocks.fetchedAfterMount,
    refetch: mocks.refetch,
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
function renderMappings(params = new URLSearchParams()) {
  const client = new QueryClient();
  return render(<DirectoryRoleMappings />, {
    wrapper: ({ children }) => (
      <MemoryRouter initialEntries={[`/?${params}`]}>
        <QueryClientProvider client={client}>
          <TooltipProvider>{children}</TooltipProvider>
        </QueryClientProvider>
      </MemoryRouter>
    ),
  });
}

beforeEach(() => {
  mocks.save.mockReset();
  mocks.refetch.mockReset();
  mocks.toastError.mockReset();
  mocks.refetch.mockImplementation(async () => ({
    data: { mappings: mocks.mappings },
  }));
  mocks.fetchedAfterMount = true;
  mocks.mappings = [mapping("Base"), mapping("Tools")];
  window.sessionStorage.clear();
});
afterEach(cleanup);

describe("directory source role sets", () => {
  it("shows both roles and only offers unmapped roles to add", async () => {
    renderMappings();
    expect(screen.getByText("Base")).toBeDefined();
    expect(screen.getByText("Tools")).toBeDefined();
    expect(screen.queryByRole("button", { name: "Add Base" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Add Tools" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Add Support" }));
    await waitFor(() => expect(mocks.save).toHaveBeenCalledOnce());
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

  it("removes one role without removing the others", async () => {
    renderMappings();
    fireEvent.click(
      screen.getByRole("button", { name: "Remove Base from Engineering" }),
    );
    await waitFor(() => expect(mocks.save).toHaveBeenCalledOnce());
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

  it("removes the final role with an empty set", async () => {
    mocks.mappings = [mapping("Base")];
    renderMappings();
    fireEvent.click(
      screen.getByRole("button", { name: "Remove Base from Engineering" }),
    );
    await waitFor(() => expect(mocks.save).toHaveBeenCalledOnce());
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

  it("waits for post-mount data before mapping a created role", () => {
    const editorParams = startCreateRoleFlow(
      { sourceKind: "group", directoryGroupId: "group-1" },
      "Support",
    );
    const params = completeCreateRoleFlow(
      editorParams,
      "role:organization:Support",
    );
    mocks.fetchedAfterMount = false;
    const view = renderMappings(params);
    expect(mocks.save).not.toHaveBeenCalled();

    mocks.mappings = [mapping("Tools")];
    mocks.fetchedAfterMount = true;
    view.rerender(<DirectoryRoleMappings />);
    expect(mocks.save).toHaveBeenCalledWith(
      {
        request: {
          setDirectoryRoleMappingsForm: {
            sourceKind: "group",
            directoryGroupId: "group-1",
            roleUrns: ["role:organization:Tools", "role:organization:Support"],
          },
        },
      },
      expect.any(Object),
    );
  });

  it("adds to the refreshed set rather than the rendered set", async () => {
    renderMappings();
    mocks.refetch.mockResolvedValue({ data: { mappings: [mapping("Tools")] } });
    fireEvent.click(screen.getByRole("button", { name: "Add Support" }));
    await waitFor(() => expect(mocks.save).toHaveBeenCalledOnce());
    expect(
      mocks.save.mock.calls[0]?.[0].request.setDirectoryRoleMappingsForm
        .roleUrns,
    ).toEqual(["role:organization:Tools", "role:organization:Support"]);
  });

  it("preserves roles added since the row rendered when removing", async () => {
    renderMappings();
    mocks.refetch.mockResolvedValue({
      data: {
        mappings: [mapping("Base"), mapping("Tools"), mapping("Support")],
      },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Remove Base from Engineering" }),
    );
    await waitFor(() => expect(mocks.save).toHaveBeenCalledOnce());
    expect(
      mocks.save.mock.calls[0]?.[0].request.setDirectoryRoleMappingsForm
        .roleUrns,
    ).toEqual(["role:organization:Tools", "role:organization:Support"]);
  });

  it("does not save when the mapping refresh fails", async () => {
    renderMappings();
    mocks.refetch.mockRejectedValue(new Error("Refresh failed"));
    fireEvent.click(screen.getByRole("button", { name: "Add Support" }));
    await waitFor(() =>
      expect(mocks.toastError).toHaveBeenCalledWith("Refresh failed"),
    );
    expect(mocks.save).not.toHaveBeenCalled();
  });
});
