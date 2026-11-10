import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
const state = vi.hoisted(() => ({
  mutate: vi.fn(),
  updateRoles: vi.fn(),
  failed: false,
}));
vi.mock("@tanstack/react-query", () => ({ useQueryClient: () => ({}) }));
vi.mock("@gram/client/react-query/resourceAudience.js", () => ({
  invalidateAllResourceAudience: vi.fn(),
  useResourceAudience: () => ({
    isError: state.failed,
    isLoading: false,
    isFetching: false,
    data: {
      version: "rules-v1",
      entries: [
        {
          principalUrn: "role:organization:engineer",
          kind: "role",
          displayName: "Engineer",
          level: "use",
          appliesTo: "all_resources",
          memberIds: ["selected-person", "other-person"],
        },
        {
          principalUrn: "role:organization:unrelated",
          kind: "role",
          displayName: "Unrelated",
          level: "use",
          appliesTo: "all_resources",
          memberIds: ["other-person"],
        },
        {
          principalUrn: "user:other-person",
          kind: "user",
          displayName: "Other person",
          level: "use",
          appliesTo: "resource",
          tools: ["read_issue"],
        },
      ],
    },
  }),
}));
vi.mock("@gram/client/react-query/setResourceAudience.js", () => ({
  useSetResourceAudienceMutation: () => ({
    mutate: state.mutate,
    isPending: false,
  }),
}));
vi.mock("@gram/client/react-query/roles.js", () => ({
  invalidateAllRoles: vi.fn(),
  useRoles: () => ({
    data: {
      roles: [
        { id: "engineer", principalUrn: "role:organization:engineer" },
        { id: "unrelated", principalUrn: "role:organization:unrelated" },
      ],
    },
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
  }),
}));
vi.mock("@gram/client/react-query/updateMemberRoles.js", () => ({
  useUpdateMemberRolesMutation: () => ({
    mutate: state.updateRoles,
    isPending: false,
  }),
}));
import { RemoveAssignmentDialog } from "./RemoveAssignmentDialog";
const server = {
  id: "server-a",
  name: "GitHub",
  projectId: "project-a",
  projectSlug: "default",
  slug: "github",
};
const member = {
  id: "selected-person",
  principalUrn: "user:selected-person",
  name: "Morgan",
  email: "morgan@example.test",
  roleIds: ["engineer", "member"],
  directoryRoleIds: [],
  joinedAt: new Date(),
};
afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  state.failed = false;
});
it("creates a version-checked role exclusion while preserving unrelated grants", () => {
  render(
    <RemoveAssignmentDialog
      server={server}
      member={member}
      onClose={() => {}}
    />,
  );
  expect(
    (
      screen.getByRole("button", {
        name: "Remove access",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
  fireEvent.click(screen.getByRole("checkbox", { name: /^Engineer/ }));
  fireEvent.click(screen.getByRole("button", { name: "Remove access" }));
  expect(state.mutate).toHaveBeenCalledWith({
    request: {
      setResourceAudienceForm: {
        resourceKind: "mcp",
        resourceId: "server-a",
        expectedVersion: "rules-v1",
        entries: [
          {
            principalUrn: "user:other-person",
            level: "use",
            tools: ["read_issue"],
            dispositions: undefined,
          },
          ...["blocked"].map((level) => ({
            principalUrn: "role:organization:engineer",
            level,
            tools: [],
            dispositions: [],
          })),
        ],
      },
    },
  });
});
it("can block only this user without adding role exclusions", () => {
  render(
    <RemoveAssignmentDialog
      server={server}
      member={member}
      onClose={() => {}}
    />,
  );
  fireEvent.click(
    screen.getByRole("checkbox", { name: /^Block only this user/ }),
  );
  fireEvent.click(screen.getByRole("button", { name: "Remove access" }));
  const entries =
    state.mutate.mock.calls[0]?.[0].request.setResourceAudienceForm.entries;
  expect(
    entries.filter(
      (entry: { principalUrn: string }) =>
        entry.principalUrn === "user:selected-person",
    ),
  ).toHaveLength(1);
  expect(
    entries.some(
      (entry: { principalUrn: string }) =>
        entry.principalUrn === "role:organization:engineer",
    ),
  ).toBe(false);
});
it("does not allow removal when current rules could not be loaded", () => {
  state.failed = true;
  render(
    <RemoveAssignmentDialog
      server={server}
      member={member}
      onClose={() => {}}
    />,
  );
  expect(screen.getByRole("alert").textContent).toContain("Couldn’t load");
  expect(
    (
      screen.getByRole("button", {
        name: "Remove access",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
});

it("shows only the selected person's roles and can remove their membership", () => {
  render(
    <RemoveAssignmentDialog
      server={server}
      member={member}
      onClose={() => {}}
    />,
  );
  expect(screen.queryByText("Unrelated")).toBeNull();
  fireEvent.click(
    screen.getByRole("checkbox", { name: "Remove Morgan from Engineer" }),
  );
  fireEvent.click(screen.getByRole("button", { name: "Remove from role" }));
  expect(state.updateRoles).toHaveBeenCalledWith({
    request: {
      updateMemberRolesForm: { userId: "selected-person", roleIds: ["member"] },
    },
  });
  expect(state.mutate).not.toHaveBeenCalled();
});
it("does not offer removal of directory-managed memberships", () => {
  render(
    <RemoveAssignmentDialog
      server={server}
      member={{ ...member, directoryRoleIds: ["engineer"] }}
      onClose={() => {}}
    />,
  );
  expect(
    (
      screen.getByRole("checkbox", {
        name: "Remove Morgan from Engineer",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
});
