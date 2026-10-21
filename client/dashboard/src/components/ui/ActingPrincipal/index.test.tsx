import { render, screen, cleanup } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ActingPrincipal } from ".";
import { actingPrincipalText } from "./text";

vi.mock("@/lib/useIdentityHref", () => ({
  useIdentityHrefBuilder: () => () => "/identities/x",
}));

afterEach(cleanup);

function setup(node: React.ReactNode) {
  render(<MemoryRouter>{node}</MemoryRouter>);
}

describe("ActingPrincipal", () => {
  it("names the agent and the person it acted for", () => {
    setup(
      <ActingPrincipal
        agent={{ id: "a1", name: "Release assistant" }}
        authorizer={{ id: "u1", name: "Amara Okafor" }}
      />,
    );
    expect(screen.getByText("Release assistant")).toBeTruthy();
    expect(screen.getByText(/on behalf of/)).toBeTruthy();
    expect(screen.getByText("Amara Okafor")).toBeTruthy();
  });

  it("names the agent alone when no one authorized it", () => {
    // A key or a federated platform signs in as the agent and no human
    // stands behind the call. "on behalf of unknown" would imply a person we
    // failed to look up.
    setup(<ActingPrincipal agent={{ id: "a1", name: "Release assistant" }} />);
    expect(screen.queryByText(/on behalf of/)).toBeNull();
  });

  it("falls back to identifiers rather than rendering nothing", () => {
    setup(
      <ActingPrincipal agent={{ id: "a1" }} authorizer={{ id: "user_x" }} />,
    );
    expect(screen.getByText("agent:a1")).toBeTruthy();
    expect(screen.getByText("user_x")).toBeTruthy();
  });

  it("gives the same relation as plain text", () => {
    expect(
      actingPrincipalText({ id: "a1", name: "Bot" }, { id: "u1", name: "Ada" }),
    ).toBe("Bot on behalf of Ada");
    expect(actingPrincipalText({ id: "a1", name: "Bot" })).toBe("Bot");
  });
});
