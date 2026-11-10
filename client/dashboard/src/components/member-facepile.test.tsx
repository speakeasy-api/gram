import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { MemberFacepile } from "./member-facepile";

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: () => true,
    hasAnyScope: () => true,
    hasAllScopes: () => true,
    isLoading: false,
    grants: [],
    error: null,
  }),
}));

afterEach(cleanup);

describe("MemberFacepile custom trigger", () => {
  it.each(["Enter", " "])("opens its roster with %s", (key) => {
    render(
      <MemoryRouter>
        <MemberFacepile
          members={[
            {
              id: "member-1",
              name: "Test Member",
              email: "member@example.test",
            },
          ]}
          renderTrigger={({ label, onClick, onKeyDown }) => (
            <span
              role="button"
              tabIndex={0}
              aria-label={`Show ${label}`}
              onClick={onClick}
              onKeyDown={onKeyDown}
            >
              {label}
            </span>
          )}
        />
      </MemoryRouter>,
    );

    fireEvent.keyDown(screen.getByRole("button", { name: "Show 1 member" }), {
      key,
    });

    expect(screen.getByText("member@example.test")).toBeTruthy();
  });
});

it("searches the member dropdown and selects a member without navigation", async () => {
  const onMemberSelect = vi.fn();
  const members = [
    { id: "lee", name: "Lee", email: "lee@example.test" },
    { id: "sam", name: "Sam", email: "sam@example.test" },
    ...Array.from({ length: 4 }, (_, i) => ({
      id: `other-${i}`,
      name: `Other ${i}`,
      email: `other-${i}@example.test`,
    })),
  ];
  render(
    <MemoryRouter>
      <TooltipProvider>
        <MemberFacepile
          members={members}
          memberActionLabel="View assignments"
          onMemberSelect={(member) => {
            onMemberSelect(member);
          }}
        />
      </TooltipProvider>
    </MemoryRouter>,
  );
  await userEvent.click(screen.getByRole("button", { name: "6 members" }));
  await userEvent.type(
    screen.getByPlaceholderText("Search members…"),
    "sam@example.test",
  );
  expect(screen.getAllByRole("option")).toHaveLength(1);
  await userEvent.click(screen.getByRole("option", { name: /Sam/ }));
  expect(onMemberSelect).toHaveBeenCalledWith(members[1]);
  expect(screen.queryByPlaceholderText("Search members…")).toBeNull();
});

it("always shows compact search and the edit-role action for a small roster", async () => {
  const members = Array.from({ length: 5 }, (_, i) => ({
    id: `member-${i}`,
    name: `Member ${i}`,
    email: `member-${i}@example.test`,
  }));
  const editRole = vi.fn();
  const selectMember = vi.fn();
  render(
    <MemoryRouter>
      <TooltipProvider>
        <MemberFacepile
          members={members}
          memberActionLabel="View assignments"
          onEditRole={() => {
            editRole();
          }}
          onMemberSelect={(member) => {
            selectMember(member);
          }}
        />
      </TooltipProvider>
    </MemoryRouter>,
  );
  await userEvent.click(screen.getByRole("button", { name: "5 members" }));
  expect(screen.getByPlaceholderText("Search members…")).toBeTruthy();
  expect(screen.getAllByRole("option")).toHaveLength(5);
  expect(
    screen
      .getAllByRole("option")
      .every((option) =>
        option.getAttribute("aria-label")?.includes("View assignments"),
      ),
  ).toBe(true);
  expect(screen.getAllByText("View assignments").length).toBeGreaterThan(0);
  screen.getByRole("button", { name: "Edit role" }).focus();
  await userEvent.keyboard("{Enter}");
  expect(editRole).toHaveBeenCalledOnce();
  expect(selectMember).not.toHaveBeenCalled();
});
