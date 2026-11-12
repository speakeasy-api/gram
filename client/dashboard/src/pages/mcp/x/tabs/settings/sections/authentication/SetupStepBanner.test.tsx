import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { SetupStepBanner } from "./SetupStepBanner";

const readiness = vi.hoisted(() => ({
  ready: false,
  loading: false,
  unavailable: false,
}));

vi.mock("@/hooks/useMcpServerAuthenticationReadiness", () => ({
  useMcpServerAuthenticationReadiness: () => readiness,
}));

const linear = { id: "server-1", name: "Linear" } as McpServer;

function renderAt(search: string): void {
  render(
    <MemoryRouter initialEntries={[`/settings${search}#authentication`]}>
      <SetupStepBanner mcpServer={linear} />
    </MemoryRouter>,
  );
}

afterEach(() => {
  cleanup();
  readiness.ready = false;
  readiness.loading = false;
  readiness.unavailable = false;
});

describe("SetupStepBanner", () => {
  it("says to add the API key when credentials are left", () => {
    renderAt("?setup=credentials");

    expect(screen.getByText("One step left")).toBeTruthy();
    expect(
      screen.getByText(/Add the API key Linear needs.*Enter it below/),
    ).toBeTruthy();
  });

  it("says to set up the OAuth client when the OAuth client is left", () => {
    renderAt("?setup=oauth-client");

    expect(screen.getByText("One step left")).toBeTruthy();
    expect(
      screen.getByText(/Set up the OAuth client Linear needs.*client details/),
    ).toBeTruthy();
    expect(screen.queryByText(/Sign in to Linear/)).toBeNull();
  });

  it("says to sign in when sign-in is left", () => {
    renderAt("?setup=sign-in");

    expect(screen.getByText(/Sign in to Linear so Speakeasy/)).toBeTruthy();
  });

  it("says the server is ready once the step is done", () => {
    readiness.ready = true;
    renderAt("?setup=credentials");

    expect(
      screen.getByText(
        "Linear is ready to use. Go back to your chat to keep going.",
      ),
    ).toBeTruthy();
    expect(screen.queryByText("One step left")).toBeNull();
  });

  it("shows nothing while readiness is still loading", () => {
    readiness.loading = true;
    renderAt("?setup=sign-in");

    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.queryByText(/Sign in to Linear/)).toBeNull();
  });

  it("shows nothing when readiness is unavailable", () => {
    readiness.unavailable = true;
    renderAt("?setup=credentials");

    expect(screen.queryByText("One step left")).toBeNull();
    expect(screen.queryByText(/Add the API key/)).toBeNull();
  });

  it.each(["", "?setup=other"])(
    "shows nothing without a known setup step (%s)",
    (search) => {
      renderAt(search);

      expect(screen.queryByText("One step left")).toBeNull();
      expect(screen.queryByText(/ready to use/)).toBeNull();
    },
  );
});
