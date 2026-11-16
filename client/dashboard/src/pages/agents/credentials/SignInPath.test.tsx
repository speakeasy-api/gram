import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { SignInPath } from "./SignInPath";

afterEach(cleanup);

// The path renders across the page and, at phone width, down it; jsdom
// applies no media queries, so each arrow is in the tree twice.
const lit = () =>
  screen
    .getAllByText("This step")
    .map((badge) => badge.parentElement?.textContent);

it("lights the agent's sign-in and says it never reaches the servers", () => {
  render(<SignInPath hop="agent" />);
  expect(screen.getByRole("figure").getAttribute("aria-label")).toBe(
    "Agent sign-in: agent to Speakeasy",
  );
  expect(lit()).toEqual(["Agent sign-inThis step", "Agent sign-inThis step"]);
  expect(
    screen.getByText(/only gets the agent into Speakeasy/).textContent,
  ).toContain("nothing to do with your MCP servers");
});

it("lights Speakeasy's sign-in to the servers on MCP sign-in", () => {
  render(<SignInPath hop="mcp" />);
  expect(lit()).toEqual(["MCP sign-inThis step", "MCP sign-inThis step"]);
  expect(screen.getByText(/which you choose next/)).toBeTruthy();
});
