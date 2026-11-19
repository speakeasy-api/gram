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
        credentials: [],
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

it.each([
  { field: "applies_to", value: "future_reach", level: "manage" },
  { field: "applies_to", value: "constructor", level: "blocked" },
  { field: "effect", value: "future_effect", level: "manage" },
  { field: "effect", value: "__proto__", level: "blocked" },
])(
  "renders unknown $field with $level neutrally alongside known rules",
  ({ field, value, level }) => {
    const rule = {
      principal_urn: "user:example",
      display_name: "Example person",
      kind: "user",
      applies_to: "resource",
      effect: "allows",
      via_directory_mapping: false,
    };
    query.mockReturnValue({
      data: ExplainResourceAccessResult$inboundSchema.parse({
        visibility: "private",
        credentials: [],
        levels: [
          {
            level: "use",
            allowed: true,
            tool_access: "all",
            rules: [
              { ...rule, level: "manage" },
              { ...rule, level, [field]: value },
              { ...rule, level: "blocked", effect: "overridden" },
            ],
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
    expect(
      screen.getByText("Example person can use Example server with all tools."),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Show 3 rules" }));
    const unknownRow = screen.getByText("Unknown access").closest("tr")!;
    const unknownDescription = within(unknownRow).getAllByRole("cell")[1]!;
    expect(within(unknownDescription).getByText("Unknown")).toBeTruthy();
    expect(
      within(unknownDescription).queryByText(/^(Grants|Blocks)$/),
    ).toBeNull();
    if (field === "applies_to") {
      expect(within(unknownRow).getByText("Allows")).toBeTruthy();
      expect(
        within(unknownRow).getByText("Rule reach unavailable."),
      ).toBeTruthy();
      expect(
        within(unknownRow).queryByText(
          "Inherited from a rule covering more servers.",
        ),
      ).toBeNull();
    } else {
      expect(
        within(unknownRow).getByText("Access effect unavailable."),
      ).toBeTruthy();
    }
    expect(
      within(
        screen.getByText("Manage · includes Connect").closest("tr")!,
      ).getByText("Grants"),
    ).toBeTruthy();
    const blockLabel = screen.getByText("Connect · All tools");
    expect(within(blockLabel.closest("tr")!).getByText("Blocks")).toBeTruthy();
    expect(blockLabel.parentElement?.classList.contains("line-through")).toBe(
      true,
    );
  },
);

it("flags a person who can connect but has no credential to sign in with", () => {
  const credential = {
    auth_client_id: "00000000-0000-4000-8000-000000000001",
    name: "Reporting bot",
    client_type: "service_account",
    enabled: true,
    allowed: false,
  };
  query.mockReturnValue({
    data: ExplainResourceAccessResult$inboundSchema.parse({
      visibility: "private",
      levels: [{ level: "use", allowed: true, tool_access: "all", rules: [] }],
      credentials: [
        {
          ...credential,
          rules: [
            {
              principal_urn: "role:contractors",
              display_name: "Contractors",
              kind: "everyone",
              applies_to: "credential_type",
              effect: "blocks",
              via_directory_mapping: false,
            },
          ],
        },
        {
          ...credential,
          auth_client_id: "00000000-0000-4000-8000-000000000002",
          name: "Shared key",
          client_type: "static_credential",
          rules: [],
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
  expect(
    screen.getByText(
      "Example person can connect but has no credential to sign in upstream with.",
    ),
  ).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Show 2 credentials" }));
  const blocked = screen.getByText("Reporting bot").closest("tr")!;
  expect(within(blocked).getByText("Blocked")).toBeTruthy();
  expect(
    within(blocked).getByText("Blocks every credential of this kind"),
  ).toBeTruthy();
  const unnamed = screen.getByText("Shared key").closest("tr")!;
  expect(within(unnamed).getByText("No rule allows it")).toBeTruthy();
});
