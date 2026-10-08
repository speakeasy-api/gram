import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";

import { RoleLink } from "./role-link";

vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    access: { roles: { href: () => "/acme/access/roles" } },
  }),
}));

afterEach(cleanup);

function renderLink(props: { roleId?: string; principalUrn?: string }) {
  render(
    <MemoryRouter>
      <RoleLink {...props}>Support Desk</RoleLink>
    </MemoryRouter>,
  );
}

describe("RoleLink", () => {
  it("links a role id to its edit page", () => {
    renderLink({ roleId: "role-1" });
    expect(
      screen.getByRole("link", { name: "Support Desk" }).getAttribute("href"),
    ).toBe("/acme/access/roles/role-1/edit");
  });

  it("reads the id from a role principal URN", () => {
    renderLink({ principalUrn: "role:organization:role-2" });
    expect(
      screen.getByRole("link", { name: "Support Desk" }).getAttribute("href"),
    ).toBe("/acme/access/roles/role-2/edit");
  });

  it("leaves the name plain without a role id", () => {
    renderLink({ principalUrn: "user:someone" });
    expect(screen.queryByRole("link")).toBeNull();
    expect(screen.getByText("Support Desk")).toBeTruthy();
  });
});
