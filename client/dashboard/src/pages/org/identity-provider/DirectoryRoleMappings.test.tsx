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
  pendingMappingFromParams,
  completeCreateRoleFlow,
  startCreateRoleFlow,
} from "./directoryMappingFlow";

import type { DirectoryRoleMapping } from "@gram/client/models/components/directoryrolemapping.js";
import { DirectoryRoleMappings } from "./DirectoryRoleMappings";
import { MemoryRouter } from "react-router";
import type { Role } from "@gram/client/models/components/role.js";
import { invalidateDirectoryMappingAccess } from "./invalidateDirectoryMappingAccess";
import { TooltipProvider } from "@/components/ui/Tooltip";

const mocks = vi.hoisted(() => ({
  save: vi.fn(),
  remove: vi.fn(),
  saveOptions: [] as { retry?: boolean; onError?: (error: Error) => void }[],
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
  useSetDirectoryRoleMappingsMutation: (
    options: (typeof mocks.saveOptions)[number],
  ) => {
    mocks.saveOptions.push(options);
    return { mutate: mocks.save, isPending: false };
  },
  mutationKeySetDirectoryRoleMappings: () => ["set-mappings"],
}));
vi.mock("@gram/client/react-query/deleteDirectoryRoleMapping.js", () => ({
  useDeleteDirectoryRoleMappingMutation: () => ({
    mutate: mocks.remove,
    isPending: false,
  }),
  mutationKeyDeleteDirectoryRoleMapping: () => ["delete-mapping"],
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
  mocks.saveOptions = [];
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
          expectedRoleUrns: [
            "role:organization:Base",
            "role:organization:Tools",
          ],
          roleUrns: roles.map((role) => role.principalUrn),
        },
      },
    });
  });

  it.each([false, true])(
    "removes an exact mapping ID, final role: %s",
    async (final) => {
      if (final) mocks.mappings = [mapping("Base")];
      renderMappings();
      fireEvent.click(
        screen.getByRole("button", { name: "Remove Base from Engineering" }),
      );
      expect(mocks.remove).toHaveBeenCalledWith({ request: { id: "Base" } });
      expect(mocks.save).not.toHaveBeenCalled();
    },
  );

  it("removes multiple mappings for a vanished attribute value by ID", () => {
    mocks.mappings = ["Base", "Tools"].map((name) => ({
      ...mapping(name),
      sourceKind: "attribute",
      directoryGroupId: undefined,
      attributeKey: "department",
      attributeValue: "Former department",
    }));
    const view = renderMappings();
    fireEvent.click(screen.getByRole("button", { name: /Map by attribute/ }));
    fireEvent.click(
      screen.getByRole("button", {
        name: "Remove Base from department = Former department",
      }),
    );
    mocks.mappings = mocks.mappings.filter((mapping) => mapping.id !== "Base");
    view.rerender(<DirectoryRoleMappings />);
    fireEvent.click(
      screen.getByRole("button", {
        name: "Remove Tools from department = Former department",
      }),
    );
    expect(mocks.remove.mock.calls).toEqual([
      [{ request: { id: "Base" } }],
      [{ request: { id: "Tools" } }],
    ]);
    expect(mocks.save).not.toHaveBeenCalled();
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
            expectedRoleUrns: ["role:organization:Tools"],
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
    expect(
      mocks.save.mock.calls[0]?.[0].request.setDirectoryRoleMappingsForm
        .expectedRoleUrns,
    ).toEqual(["role:organization:Tools"]);
  });

  it("does not overwrite a concurrent addition when removing", () => {
    renderMappings();
    mocks.mappings.push(mapping("Support"));
    fireEvent.click(
      screen.getByRole("button", { name: "Remove Base from Engineering" }),
    );
    expect(mocks.remove).toHaveBeenCalledWith({ request: { id: "Base" } });
    expect(mocks.save).not.toHaveBeenCalled();
    expect(mocks.refetch).not.toHaveBeenCalled();
  });

  it("sends an empty observed set when adding to an unmapped source", async () => {
    mocks.mappings = [];
    renderMappings();
    fireEvent.click(screen.getByRole("button", { name: "Add Support" }));
    await waitFor(() => expect(mocks.save).toHaveBeenCalledOnce());
    expect(
      mocks.save.mock.calls[0]?.[0].request.setDirectoryRoleMappingsForm,
    ).toMatchObject({
      expectedRoleUrns: [],
      roleUrns: ["role:organization:Support"],
    });
  });

  it("reports a conflict and refreshes without retrying the stale addition", async () => {
    const view = renderMappings();
    fireEvent.click(screen.getByRole("button", { name: "Add Support" }));
    await waitFor(() => expect(mocks.save).toHaveBeenCalledOnce());
    const options = mocks.saveOptions.at(-1)!;
    expect(options.retry).toBe(false);
    options.onError?.(new Error("Role mappings changed"));
    expect(mocks.toastError).toHaveBeenCalledWith("Role mappings changed");
    expect(invalidateDirectoryMappingAccess).toHaveBeenCalled();
    mocks.mappings = [mapping("Tools")];
    view.rerender(<DirectoryRoleMappings />);
    expect(mocks.save).toHaveBeenCalledOnce();
  });

  it("retains a conflicted create-role return without automatically retrying", async () => {
    const params = completeCreateRoleFlow(
      startCreateRoleFlow(
        { sourceKind: "group", directoryGroupId: "group-1" },
        "Support",
      ),
      "role:organization:Support",
    );
    const view = renderMappings(params);
    expect(mocks.save).toHaveBeenCalledOnce();
    const options = mocks.saveOptions[0]!;
    expect(options.retry).toBe(false);
    options.onError?.(new Error("Role mappings changed"));
    expect(mocks.toastError).toHaveBeenCalledWith("Role mappings changed");
    expect(mocks.refetch).toHaveBeenCalledOnce();
    mocks.mappings = [mapping("Tools")];
    view.rerender(<DirectoryRoleMappings />);
    expect(mocks.save).toHaveBeenCalledOnce();
    expect(
      pendingMappingFromParams(params, mocks.mappings)?.form,
    ).toMatchObject({
      expectedRoleUrns: ["role:organization:Tools"],
      roleUrns: ["role:organization:Tools", "role:organization:Support"],
    });
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
