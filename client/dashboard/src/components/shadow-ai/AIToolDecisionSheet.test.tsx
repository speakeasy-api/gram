import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { detection } from "@/pages/security/shadow-ai/access-fixtures";
import { AIToolDecisionSheet } from "./AIToolDecisionSheet";

vi.mock("@gram/client/react-query/setAIToolDecision.js", () => ({
  useSetAIToolDecisionMutation: () => ({ mutate: vi.fn(), isPending: false }),
}));

afterEach(cleanup);

function renderSheet(access: Parameters<typeof detection>[0]) {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <AIToolDecisionSheet
        detection={detection(access)}
        open
        onOpenChange={() => {}}
      />
    </QueryClientProvider>,
  );
}

describe("AIToolDecisionSheet", () => {
  it("says a block on a removed tool still applies and can be cleared", () => {
    renderSheet({
      access: {
        state: "blocked",
        decision: "blocked",
        enforceable: true,
        catalogStatus: "removed",
      },
    });

    expect(
      screen.getByText(
        "Removed from the Speakeasy catalog. This block still applies.",
      ),
    ).toBeTruthy();
    expect(
      screen.getByText(
        "You can change or clear this decision. Once you clear it, you cannot add a new one.",
      ),
    ).toBeTruthy();
    expect(
      screen
        .getByRole("button", { name: "Save decision" })
        .hasAttribute("disabled"),
    ).toBe(false);
  });

  it("explains a block that applies through turned-off sign-in documents", () => {
    renderSheet({
      access: {
        state: "blocked",
        decision: "blocked",
        enforceable: true,
        catalogStatus: "documents_disabled",
      },
    });

    expect(
      screen.getByText(
        "Sign-in documents are turned off for this client. This block still applies.",
      ),
    ).toBeTruthy();
  });

  it("adds no note for a tool the catalog lists as usual", () => {
    renderSheet({});

    expect(screen.queryByText(/still applies/)).toBeNull();
  });
});
