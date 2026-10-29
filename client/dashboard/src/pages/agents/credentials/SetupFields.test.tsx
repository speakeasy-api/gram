import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, expect, it, vi } from "vitest";
import { ComputedStatus } from "./SetupFields";
import type { SetupValues } from "./workloadTokenEndpoints";

vi.mock("@/routes", () => ({
  useRoutes: ({ projectSlug }: { projectSlug: string }) => ({
    mcp: {
      details: {
        settings: {
          href: (param: string) =>
            `/projects/${projectSlug}/mcp/${param}/settings`,
        },
      },
    },
  }),
}));
afterEach(cleanup);

const github = {
  id: "gh",
  name: "GitHub",
  mcpUrl: "https://gram.example.com/mcp/github",
  issuerId: "private",
  projectSlug: "default",
  slug: "github",
};
const linear = {
  id: "ln",
  name: "Linear",
  mcpUrl: "https://gram.example.com/mcp/linear",
  issuerId: "other",
  projectSlug: "default",
  slug: "linear",
};

function setupValues(overrides: Partial<SetupValues>): SetupValues {
  return {
    values: { token_endpoint: "https://gram.example.com/oauth/usi/org/token" },
    unavailableReason: null,
    endpointGroups: [
      { label: "Organization", options: [{ id: "org", label: "org" }] },
    ],
    selectedEndpointId: "org",
    onEndpointChange: vi.fn<(id: string) => void>(),
    refreshAndSelect: vi.fn<(id: string) => Promise<void>>(),
    coveredServers: [],
    uncoveredServers: [],
    ...overrides,
  };
}

it("names each server the selected issuer's token won't work for, with its fix", () => {
  const onEndpointChange = vi.fn<(id: string) => void>();
  render(
    <MemoryRouter>
      <ComputedStatus
        setupValues={setupValues({
          onEndpointChange,
          uncoveredServers: [
            { server: linear, reason: "another_issuer" },
            { server: github, reason: "not_shared" },
          ],
        })}
      />
    </MemoryRouter>,
  );

  expect(
    screen.getByText("This token won't work for 2 of this agent's servers"),
  ).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Use its issuer" }));
  expect(onEndpointChange).toHaveBeenCalledWith("other");
  expect(
    screen
      .getByRole("link", { name: "Open sign-in settings" })
      .getAttribute("href"),
  ).toBe("/projects/default/mcp/github/settings#authentication");
});

it("lists the servers behind an unavailable token endpoint", () => {
  render(
    <MemoryRouter>
      <ComputedStatus
        setupValues={setupValues({
          values: undefined,
          unavailableReason: "No shared issuer.",
          endpointGroups: [],
          selectedEndpointId: "",
          uncoveredServers: [{ server: github, reason: "not_shared" }],
        })}
      />
    </MemoryRouter>,
  );

  expect(screen.getByText("No shared issuer.")).toBeTruthy();
  expect(screen.getByText("GitHub")).toBeTruthy();
  expect(
    screen.getByRole("link", { name: "Open sign-in settings" }),
  ).toBeTruthy();
});

it("stays quiet when the selected issuer's token works for every server", () => {
  render(
    <MemoryRouter>
      <ComputedStatus setupValues={setupValues({ coveredServers: [github] })} />
    </MemoryRouter>,
  );
  expect(screen.queryByText(/This token won't work/)).toBeNull();
});
