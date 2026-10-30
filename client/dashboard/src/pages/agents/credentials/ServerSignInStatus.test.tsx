import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { ServerSignInAction, ServerSignInNote } from "./ServerSignInStatus";

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
