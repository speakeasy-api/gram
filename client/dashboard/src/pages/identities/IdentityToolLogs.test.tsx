import { cleanup, render, screen } from "@testing-library/react";
import type { IdentityModel } from "@gram/client/models/components/identitymodel.js";
import type { ToolUsageUserFilter } from "@gram/client/models/components/toolusageuserfilter.js";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { IdentityToolLogsPanel } from "./IdentityToolLogs";

const state = vi.hoisted(() => ({
  projectWide: false,
  // The scope each render handed to the view, so a test can prove the rows
  // were pinned to the identity rather than left project-wide.
  scopes: [] as ToolUsageUserFilter[][],
}));

vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({ user: { id: "viewer", email: "viewer@example.com" } }),
}));

vi.mock("@/components/observe/toolLogsAccess", async (importOriginal) => ({
  ...(await importOriginal<
    typeof import("@/components/observe/toolLogsAccess")
  >()),
  useToolLogsAccess: () => ({
    canOpen: true,
    projectWide: state.projectWide,
    isLoading: false,
  }),
}));

vi.mock("@/components/observe/LogsTools", () => ({
  LogsTools: ({ scope }: { scope: { userFilters: ToolUsageUserFilter[] } }) => {
    state.scopes.push(scope.userFilters);
    return <div>Tool log rows</div>;
  },
}));

function person(
  userId: string,
  overrides: Partial<Pick<IdentityModel, "userIds" | "emails">> = {},
): IdentityModel {
  return {
    userIds: [userId],
    externalUserIds: [],
    emails: [`${userId}@example.com`],
    ...overrides,
  } as unknown as IdentityModel;
}

const NEEDS_PERMISSION = /need the View tool logs permission/;

beforeEach(() => {
  state.projectWide = false;
  state.scopes = [];
});
afterEach(cleanup);

describe("IdentityToolLogsPanel", () => {
  it("shows the viewer their own calls and their agent's without log:read", () => {
    render(<IdentityToolLogsPanel identity={person("viewer")} />);
    expect(screen.getByText("Tool log rows")).toBeTruthy();
    expect(state.scopes.at(-1)).toEqual([
      { kind: "user_id", key: "viewer" },
      { kind: "email", key: "viewer@example.com" },
    ]);
    cleanup();

    render(
      <IdentityToolLogsPanel
        agent={{ id: "agent-1", ownerUserId: "viewer" }}
      />,
    );
    expect(screen.getByText("Tool log rows")).toBeTruthy();
    expect(state.scopes.at(-1)).toEqual([{ kind: "agent_id", key: "agent-1" }]);
  });

  it("recognises the viewer by address when telemetry has no directory id for them", () => {
    render(
      <IdentityToolLogsPanel
        identity={person("viewer", {
          userIds: [],
          emails: ["Viewer@Example.com"],
        })}
      />,
    );

    expect(screen.getByText("Tool log rows")).toBeTruthy();
    expect(state.scopes.at(-1)).toEqual([
      { kind: "email", key: "Viewer@Example.com" },
    ]);
  });

  it("keeps other members, their agents and latched agents behind log:read", () => {
    render(<IdentityToolLogsPanel identity={person("someone-else")} />);
    expect(screen.getByText(NEEDS_PERMISSION)).toBeTruthy();
    cleanup();

    // Another person's identity can list the viewer's address as a linked
    // account; the directory id settles it.
    render(
      <IdentityToolLogsPanel
        identity={person("someone-else", {
          emails: ["someone-else@example.com", "viewer@example.com"],
        })}
      />,
    );
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
    expect(state.scopes).toEqual([]);
  });

  it("shows anyone's calls with log:read on the project, pinned to them", () => {
    state.projectWide = true;

    render(<IdentityToolLogsPanel identity={person("someone-else")} />);

    expect(screen.getByText("Tool log rows")).toBeTruthy();
    expect(state.scopes.at(-1)).toEqual([
      { kind: "user_id", key: "someone-else" },
      { kind: "email", key: "someone-else@example.com" },
    ]);
  });
});
