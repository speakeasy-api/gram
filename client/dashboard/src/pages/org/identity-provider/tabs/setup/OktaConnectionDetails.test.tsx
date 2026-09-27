import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { TooltipProvider } from "@/components/ui/Tooltip";
import type { OktaIdentityProviderConnection } from "@gram/client/models/components/oktaidentityproviderconnection.js";

import { ConnectionFacts } from "./OktaConnectionDetails";
import { makeChecklistItem, makeConnection } from "./testFixtures";

vi.mock("@/lib/dates", () => ({
  HumanizeDateTime: ({ date }: { date: Date }) => (
    <time>{date.toISOString()}</time>
  ),
}));

afterEach(cleanup);

const connection = makeConnection({ status: "degraded" });

function facts(value: OktaIdentityProviderConnection) {
  return (
    <TooltipProvider>
      <ConnectionFacts connection={value} />
    </TooltipProvider>
  );
}

describe("ConnectionFacts last verification", () => {
  it("does not report unrelated agent edits as degraded check times", () => {
    const { container, rerender } = render(facts(connection));
    expect(screen.getByText("Never;")).toBeTruthy();
    expect(screen.getByText("needs attention")).toBeTruthy();
    expect(container.querySelector("time")).toBeNull();

    rerender(
      facts({
        ...connection,
        agentId: "agent-id",
        updatedAt: new Date("2026-01-03T00:00:00Z"),
      }),
    );
    expect(screen.getByText("needs attention")).toBeTruthy();
    expect(container.querySelector("time")).toBeNull();
  });

  it("retains the last successful verification timestamp after degradation", () => {
    const lastVerifiedAt = new Date("2026-01-01T12:00:00Z");
    render(facts({ ...connection, lastVerifiedAt }));
    expect(screen.getByText(lastVerifiedAt.toISOString())).toBeTruthy();
    expect(screen.queryByText("needs attention")).toBeNull();
    expect(screen.queryByText(connection.updatedAt.toISOString())).toBeNull();
  });
});

describe("ConnectionFacts token protection", () => {
  it.each([
    ["pending", true, "Not checked yet"],
    ["verified", true, "Protected"],
    ["degraded", false, "Not protected"],
  ] as const)(
    "describes %s protection without implying an unchecked result",
    (status, dpopRequired, label) => {
      render(facts({ ...connection, status, dpopRequired }));
      expect(screen.getByText("Token protection (DPoP)")).toBeTruthy();
      expect(screen.getByText(label)).toBeTruthy();
      expect(screen.getByText("Public key URL (JWKS)")).toBeTruthy();
    },
  );

  it("leaves token protection unchecked when no token was observed", () => {
    render(
      facts({
        ...connection,
        dpopRequired: false,
        checklist: [makeChecklistItem("dpop", "connect")],
      }),
    );
    expect(screen.getByText("Not checked yet")).toBeTruthy();
    expect(screen.queryByText("Not protected")).toBeNull();
  });

  it("does not reuse old token protection after a failed recheck", () => {
    render(facts({ ...connection, lastError: "okta_unreachable" }));
    expect(screen.getByText("Not checked yet")).toBeTruthy();
    expect(screen.queryByText("Protected")).toBeNull();
  });
});
