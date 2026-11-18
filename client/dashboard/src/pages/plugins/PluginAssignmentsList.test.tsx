import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { PluginAssignment } from "@gram/client/models/components/pluginassignment.js";
import { cleanup, render, screen, within } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { PluginAssignmentsList } from "./PluginAssignmentsList";
import type { InstallMode } from "./install-modes";

vi.mock("@/components/identity-link", () => ({
  IdentityLink: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@/components/member-facepile", () => ({
  MemberFacepile: () => null,
}));

afterEach(cleanup);

function assignment(
  principalUrn: string,
  installMode: InstallMode,
): PluginAssignment {
  return { id: principalUrn, principalUrn, installMode, createdAt: new Date() };
}

const memberByUrn = new Map(
  ["user:a", "user:b"].map((urn) => [
    urn,
    { principalUrn: urn, email: `${urn.slice(5)}@acme.test` } as AccessMember,
  ]),
);

function renderList(assignments: PluginAssignment[]) {
  render(
    <PluginAssignmentsList
      assignments={assignments}
      roleByUrn={new Map()}
      memberByUrn={memberByUrn}
      audienceByUrn={new Map()}
    />,
  );
}

function rowFor(label: string): HTMLElement {
  return screen.getByText(label).closest("div.flex") as HTMLElement;
}

describe("PluginAssignmentsList install mode badges", () => {
  it("labels each audience row with its mode", () => {
    renderList([assignment("*", "available")]);
    expect(within(rowFor("Everyone")).getByText("Available")).toBeTruthy();
  });

  it("labels a single member with their own mode", () => {
    renderList([assignment("user:a", "required")]);
    expect(within(rowFor("1 member")).getByText("Required")).toBeTruthy();
  });

  it("labels members that share a mode with it", () => {
    renderList([
      assignment("user:a", "default"),
      assignment("user:b", "default"),
    ]);
    expect(within(rowFor("2 members")).getByText("On by default")).toBeTruthy();
  });

  it("labels members with different modes as mixed", () => {
    renderList([
      assignment("user:a", "default"),
      assignment("user:b", "available"),
    ]);
    expect(within(rowFor("2 members")).getByText("Mixed")).toBeTruthy();
  });
});
