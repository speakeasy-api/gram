import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import {
  ServerSignInAction,
  ServerSignInNote,
  SignInPicker,
} from "./ServerSignInStatus";
import type { SetupValues } from "./workloadTokenEndpoints";

vi.mock("./SetupFields", () => ({
  ServerSignInSettingsLink: () => (
    <a href="/settings">Open authentication settings</a>
  ),
  SignInSelect: () => null,
}));
vi.mock("./SwitchServerSignIn", () => ({
  SwitchServerSignIn: ({ target }: { target: { label: string } }) => (
    <button>Assign {target.label}</button>
  ),
}));
const availability = vi.hoisted(() => ({ value: "ready" }));
vi.mock("./useSwitchAvailability", () => ({
  useSwitchAvailability: () => availability.value,
}));
afterEach(() => {
  cleanup();
  availability.value = "ready";
});

const slack = {
  id: "slack",
  name: "Slack",
  mcpUrl: "https://gram.example.com/mcp/slack",
  issuerId: "private",
};

it("says nothing for a server that signs no one in", () => {
  const { container } = render(
    <ServerSignInNote name="Slack" signIn={{ status: "no_sign_in" }} />,
  );
  expect(container.textContent).toBe("");
});

it("names the sign-in a server uses and why the token won't work there", () => {
  render(
    <ServerSignInNote
      name="Slack"
      signIn={{ status: "another_issuer", label: "slack-sign-in" }}
    />,
  );
  expect(
    screen.getByText(
      /Uses a different MCP authentication provider \(slack-sign-in\)/,
    ),
  ).toBeTruthy();
});

it("offers to assign the shared sign-in to a server with its own", () => {
  render(
    <ServerSignInAction
      server={slack}
      signIn={{
        status: "not_shared",
        switchTo: { id: "org", label: "acme-workforce" },
      }}
    />,
  );
  expect(
    screen.getByRole("button", { name: "Assign acme-workforce" }),
  ).toBeTruthy();
});

it("links to the server's settings when assigning here isn't safe", () => {
  availability.value = "people_connect";
  render(
    <ServerSignInAction
      server={slack}
      signIn={{
        status: "not_shared",
        switchTo: { id: "org", label: "acme-workforce" },
      }}
    />,
  );
  expect(
    screen.getByRole("link", { name: "Open authentication settings" }),
  ).toBeTruthy();
});

it("has no fix for a server the token already works at", () => {
  const { container } = render(
    <ServerSignInAction
      server={slack}
      signIn={{ status: "covered", label: "acme-workforce" }}
    />,
  );
  expect(container.textContent).toBe("");
});

function pickerValues(selectedEndpointId: string): SetupValues {
  return {
    unavailableReason: null,
    loadState: "ready",
    endpointGroups: [
      { label: "Organization", options: [{ id: "org", label: "org" }] },
    ],
    selectedEndpointId,
    onEndpointChange: vi.fn<(id: string) => void>(),
    refreshAndSelect: vi.fn<(id: string) => Promise<void>>(),
    coveredServers: [],
    uncoveredServers: [],
    signInOf: () => ({ status: "no_sign_in" }),
  };
}

it.each([
  [[], "No servers use it yet"],
  [["GitHub", "Linear"], "Used by GitHub and Linear"],
  [
    ["GitHub", "Linear", "Notion", "Slack"],
    "Used by GitHub, Linear, Notion and 1 more",
  ],
])("names the servers the selected provider serves (%j)", (names, line) => {
  render(
    <SignInPicker
      setupValues={pickerValues("org")}
      serversUsing={() => names}
    />,
  );
  expect(screen.getByText(line)).toBeTruthy();
});
