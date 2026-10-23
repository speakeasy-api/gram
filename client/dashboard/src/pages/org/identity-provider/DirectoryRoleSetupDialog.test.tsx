import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { act } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { TooltipProvider } from "@/components/ui/Tooltip";
import type { DirectoryRoleSetupPreviewResult } from "@gram/client/models/components/directoryrolesetuppreviewresult.js";
import type { Role } from "@gram/client/models/components/role.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";

import { DirectoryRoleSetupDialog } from "./DirectoryRoleSetupDialog";

const mocks = vi.hoisted(() => ({
  mutate: vi.fn(),
  refetch: vi.fn(),
  applyOptions: {} as { onError?: (error: Error) => void },
  preview: undefined as DirectoryRoleSetupPreviewResult | undefined,
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@gram/client/react-query/directoryRoleSetupPreview.js", () => ({
  useDirectoryRoleSetupPreview: () => ({
    data: mocks.preview,
    isFetching: false,
    isError: false,
    refetch: mocks.refetch,
  }),
}));
vi.mock("@gram/client/react-query/applyDirectoryRoleSetup.js", () => ({
  useApplyDirectoryRoleSetupMutation: (options: {
    onError?: (error: Error) => void;
  }) => {
    mocks.applyOptions = options;
    return { mutate: mocks.mutate, isPending: false, isError: false };
  },
}));
vi.mock("./invalidateDirectoryMappingAccess", () => ({
  invalidateDirectoryMappingAccess: vi.fn(),
}));

const roles = [
  {
    id: "role-support",
    name: "Support",
    principalUrn: "role:support",
    grants: [{ scope: "org:read" }],
  },
] as Role[];

function renderDialog() {
  return render(<DirectoryRoleSetupDialog roles={roles} onClose={() => {}} />, {
    wrapper: ({ children }) => (
      <QueryClientProvider client={new QueryClient()}>
        <TooltipProvider>{children}</TooltipProvider>
      </QueryClientProvider>
    ),
  });
}

beforeEach(() => {
  mocks.preview = {
    items: [
      {
        source: { sourceKind: "group", directoryGroupId: "g1" },
        sourceName: "Engineering",
        directoryPeople: 4,
        existingRoleUrns: [],
        proposedRoles: [
          { roleAction: "create", roleName: "Engineering", currentGrants: [] },
        ],
        version: "v1",
      },
      {
        source: { sourceKind: "group", directoryGroupId: "g2" },
        sourceName: "Support",
        directoryPeople: 1,
        existingRoleUrns: [],
        proposedRoles: [
          {
            roleAction: "reuse",
            roleId: "role-support",
            roleUrn: "role:support",
            roleName: "Support",
            currentGrants: [{ scope: "org:read" }],
          },
        ],
        version: "v2",
      },
      {
        source: { sourceKind: "group", directoryGroupId: "g3" },
        sourceName: "Already mapped",
        directoryPeople: 2,
        existingRoleUrns: ["role:support"],
        proposedRoles: [],
        version: "v3",
      },
    ],
    hiddenAttributeKeys: [{ key: "cost_center", valueCount: 240 }],
    maxValuesPerAttributeKey: 100,
  };
});
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("DirectoryRoleSetupDialog", () => {
  it("lists unmapped sources with directory people and what each role brings", () => {
    renderDialog();
    expect(screen.getByText("Engineering")).toBeTruthy();
    expect(screen.getByText("Group, 4 directory people")).toBeTruthy();
    expect(screen.getByText("Group, 1 directory person")).toBeTruthy();
    expect(screen.queryByText("Already mapped")).toBeNull();
    expect(screen.getByText("Existing role, 1 grant")).toBeTruthy();
    expect(
      screen.getByText(
        "New roles give their members nothing until you add grants to them.",
      ),
    ).toBeTruthy();
  });

  it("names attributes left out for having too many values", () => {
    renderDialog();
    expect(screen.getByText("cost_center: 240 values")).toBeTruthy();
  });

  it("applies the selected sources with their preview versions", () => {
    renderDialog();
    fireEvent.click(screen.getByRole("button", { name: "Apply to 2 sources" }));
    expect(mocks.mutate).toHaveBeenCalledWith({
      request: {
        applyDirectoryRoleSetupForm: {
          items: [
            {
              source: { sourceKind: "group", directoryGroupId: "g1" },
              version: "v1",
              roles: [{ createName: "Engineering" }],
            },
            {
              source: { sourceKind: "group", directoryGroupId: "g2" },
              version: "v2",
              roles: [{ roleUrn: "role:support" }],
            },
          ],
          grantSet: undefined,
        },
      },
    });
  });

  it("refreshes the preview and says so when it went stale", () => {
    renderDialog();
    act(() =>
      mocks.applyOptions.onError?.(
        new GramError("changed", {
          request: new Request("https://example.test"),
          response: new Response("", { status: 409 }),
          body: "",
        }),
      ),
    );
    expect(mocks.refetch).toHaveBeenCalled();
    expect(
      screen.getByText(/changed since this preview, so nothing was saved/),
    ).toBeTruthy();
  });
});
