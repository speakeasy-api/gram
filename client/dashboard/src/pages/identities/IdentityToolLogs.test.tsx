import { cleanup, render, screen } from "@testing-library/react";
import type { IdentityModel } from "@gram/client/models/components/identitymodel.js";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { IdentityToolLogsPanel } from "./IdentityToolLogs";

const state = vi.hoisted(() => ({ projectWide: false }));

vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({ user: { id: "viewer", email: "viewer@example.com" } }),
}));

vi.mock("@/components/observe/toolLogsAccess", () => ({
  useToolLogsAccess: () => ({
    projectWide: state.projectWide,
    isLoading: false,
  }),
}));

vi.mock("@/components/observe/LogsTools", () => ({
  LogsTools: () => <div>Tool log rows</div>,
}));

function person(userId: string): IdentityModel {
  return {
    userIds: [userId],
    externalUserIds: [],
    emails: [`${userId}@example.com`],
  } as unknown as IdentityModel;
}

const NEEDS_PERMISSION = /need the View tool logs permission/;

beforeEach(() => {
  state.projectWide = false;
});
afterEach(cleanup);

describe("IdentityToolLogsPanel", () => {
  it("shows the viewer their own calls and their agent's without log:read", () => {
    render(<IdentityToolLogsPanel identity={person("viewer")} />);
    expect(screen.getByText("Tool log rows")).toBeTruthy();
    cleanup();

    render(
      <IdentityToolLogsPanel
        agent={{ id: "agent-1", ownerUserId: "viewer" }}
      />,
    );
    expect(screen.getByText("Tool log rows")).toBeTruthy();
  });

  it("keeps other members, their agents and latched agents behind log:read", () => {
    render(<IdentityToolLogsPanel identity={person("someone-else")} />);
    expect(screen.getByText(NEEDS_PERMISSION)).toBeTruthy();
    cleanup();

    render(
      <IdentityToolLogsPanel
        agent={{ id: "agent-2", ownerUserId: "someone-else" }}
      />,
    );
    expect(screen.getByText(NEEDS_PERMISSION)).toBeTruthy();
    cleanup();

    render(
      <IdentityToolLogsPanel
        agent={{
          id: "agent-1",
          ownerUserId: "viewer",
          ownerReassignmentRequiredAt: new Date("2026-01-01T00:00:00Z"),
        }}
      />,
    );
    expect(screen.getByText(NEEDS_PERMISSION)).toBeTruthy();
  });

  it("shows anyone's calls with log:read on the project", () => {
    state.projectWide = true;

    render(<IdentityToolLogsPanel identity={person("someone-else")} />);

    expect(screen.getByText("Tool log rows")).toBeTruthy();
  });
});
