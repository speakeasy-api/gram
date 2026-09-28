import { cleanup, render, screen, within } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { CatalogContext } from "./catalogContext";
import { IntegrationRequirements } from "./IntegrationRequirements";
import type { Catalog } from "./model";

afterEach(cleanup);

it("shows required integrations and named gaps together", () => {
  const catalog: Catalog = {
    capabilities: [
      { id: "session", name: "Session tracking", group: "Observability" },
      { id: "tokens", name: "Token tracking", group: "Observability" },
      { id: "cost", name: "Cost tracking", group: "Observability" },
    ],
    platforms: [
      {
        id: "cli",
        name: "Claude Code CLI",
        vendor: "Anthropic",
        family: "Claude Code",
        surface: "CLI",
      },
    ],
    methods: [
      {
        id: "device",
        name: "Device Agent",
        vendor: "Cross-platform",
        plans: "",
        claims: {},
        platforms: [
          {
            platform: "cli",
            applicability: "applicable",
            accounts: {
              personal: "supported",
              team: "supported",
              enterprise: "supported",
            },
            note: "",
            cells: {
              session: { status: "supported", note: "", verify: false },
              tokens: { status: "unknown", note: "", verify: false },
              cost: { status: "unknown", note: "", verify: false },
            },
          },
        ],
      },
    ],
  };
  render(
    <CatalogContext.Provider value={catalog}>
      <IntegrationRequirements
        platformIds={["cli"]}
        capabilityIds={["session", "tokens"]}
        methodIds={["device"]}
        scoped
      />
    </CatalogContext.Provider>,
  );
  const section = within(
    screen.getByRole("region", { name: "Integration requirements" }),
  );
  expect(
    section.getByText(
      "To cover the known supported features, you need at least:",
    ),
  ).toBeTruthy();
  expect(section.getByText("Device Agent")).toBeTruthy();
  expect(section.getByText("Remaining gaps (1 of 2)")).toBeTruthy();
  expect(section.getByRole("listitem").textContent).toContain(
    "Claude Code CLI · Token tracking",
  );
  expect(section.queryByText(/Needs verification/)).toBeNull();
});
