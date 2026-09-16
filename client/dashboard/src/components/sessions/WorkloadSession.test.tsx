import { cleanup, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { UserSession } from "@gram/client/models/components/usersession.js";
import type { UserSessionWorkload } from "@gram/client/models/components/usersessionworkload.js";

import { RevokeSessionDialog } from "./RevokeSessionDialog";
import {
  workloadAgentLabel,
  workloadIssuerLabel,
} from "@/lib/workload-session";
import { WorkloadRevocationLadder } from "./WorkloadSession";

vi.mock("@gram/client/react-query/revokeUserSession.js", () => ({
  useRevokeUserSessionMutation: () => ({ mutate: vi.fn(), isPending: false }),
}));

afterEach(cleanup);

const workload: UserSessionWorkload = {
  workloadIssuerId: "11111111-1111-4111-8111-111111111111",
  externalSubject: "repo:acme/payments-api:ref:refs/heads/main",
  workloadIssuerName: "GitHub Actions",
  workloadIssuerUrl: "https://token.actions.example.com",
  agentId: "22222222-2222-4222-8222-222222222222",
  agentName: "Deploy bot",
  agentStatus: "active",
};

describe("workload labels", () => {
  it("falls back to the issuer id when the issuer cannot be named", () => {
    expect(
      workloadIssuerLabel({ ...workload, workloadIssuerName: undefined }),
    ).toBe(`Issuer ${workload.workloadIssuerId}`);
  });

  it("states the agent's lifecycle when it refuses requests", () => {
    expect(workloadAgentLabel(workload)).toBe("Agent Deploy bot");
    expect(workloadAgentLabel({ ...workload, agentStatus: "suspended" })).toBe(
      "Agent Deploy bot (suspended)",
    );
    expect(
      workloadAgentLabel({
        ...workload,
        agentId: undefined,
        agentName: undefined,
        agentStatus: undefined,
      }),
    ).toBe("No agent assigned");
  });
});

describe("WorkloadRevocationLadder", () => {
  it("orders the controls narrowest first and marks which keep the workload out", () => {
    render(<WorkloadRevocationLadder workload={workload} />);

    const steps = screen.getAllByRole("listitem");
    expect(steps.map((step) => step.textContent)).toEqual([
      expect.stringContaining("Revoke the session"),
      expect.stringContaining("Unassign the agent from the workload"),
      expect.stringContaining("Suspend or revoke Deploy bot"),
      expect.stringContaining("Withdraw the admission"),
      expect.stringContaining("Delete the issuer GitHub Actions"),
    ]);

    const reconnects = steps.map((step) =>
      within(step).queryByText("Stops reconnecting") ? "stops" : "reconnects",
    );
    expect(reconnects).toEqual([
      "reconnects",
      "reconnects",
      "reconnects",
      "stops",
      "stops",
    ]);

    expect(
      within(steps[0]!).queryByText("Not yet available in the dashboard"),
    ).toBeNull();
    for (const step of steps.slice(1)) {
      expect(
        within(step).getByText("Not yet available in the dashboard"),
      ).not.toBeNull();
    }
  });
});

describe("RevokeSessionDialog", () => {
  it("warns that revoking a workload session does not keep it out", () => {
    const session = {
      id: "session-1",
      userSessionIssuerId: "issuer-1",
      subjectUrn: `workload:${workload.workloadIssuerId}:${workload.externalSubject}`,
      subjectType: "workload",
      subjectDisplayName: workload.externalSubject,
      jti: "jti-1",
      issuerSlug: "the-server",
      upstreams: [],
      workload,
    } as unknown as UserSession;

    render(
      <RevokeSessionDialog
        session={session}
        open
        onOpenChange={() => {}}
        onRevoked={() => {}}
      />,
    );

    expect(
      screen.getByText(/can exchange a new token and reconnect/i),
    ).not.toBeNull();
    expect(screen.getByText("Withdraw the admission")).not.toBeNull();
  });
});
