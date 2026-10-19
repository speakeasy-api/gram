import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { ExplainResourceAccessResult$inboundSchema } from "@gram/client/models/components/explainresourceaccessresult.js";
import { CheckAccessResult } from "./CheckAccessResult";

const query = vi.hoisted(() => vi.fn());
vi.mock("@gram/client/react-query/explainResourceAccess.js", () => ({
  useExplainResourceAccess: query,
}));

afterEach(cleanup);

it.each([false, true])(
  "renders mixed known and unknown access rules neutrally (unknown first: %s)",
  (unknownFirst) => {
    const rule = {
      principal_urn: "user:example",
      display_name: "Example person",
      kind: "user",
      applies_to: "resource",
      effect: "allows",
      via_directory_mapping: false,
    };
    const rules = [
      { ...rule, level: "manage" },
      { ...rule, level: "future_level" },
      { ...rule, level: "blocked_future" },
    ];
    query.mockReturnValue({
      data: ExplainResourceAccessResult$inboundSchema.parse({
        visibility: "private",
        levels: [
          {
            level: "use",
            allowed: true,
            tool_access: "all",
            rules: unknownFirst ? rules.toReversed() : rules,
          },
        ],
      }),
      isPending: false,
      isError: false,
    });
    render(
      <CheckAccessResult
        resourceId="example-server"
        userId="example-person"
        memberName="Example person"
        serverName="Example server"
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Show 3 rules" }));
    const unknownRow = screen.getByText("Unknown access").closest("tr")!;
    expect(within(unknownRow).getByText("Unknown")).toBeTruthy();
    expect(within(unknownRow).queryByText("Grants")).toBeNull();
    expect(within(unknownRow).queryByText("Blocks")).toBeNull();
    expect(screen.queryByText(/Unknown access · includes/)).toBeNull();
    const knownRow = screen
      .getByText("Manage · includes Connect")
      .closest("tr")!;
    expect(within(knownRow).getByText("Grants")).toBeTruthy();
  },
);
