import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { SetupStepBanner } from "./SetupStepBanner";

const readiness = vi.hoisted(() => ({ ready: false, loading: false }));

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
});

describe("SetupStepBanner", () => {
  it("says which header to fill in when credentials are left", () => {
    renderAt("?setup=credentials");

    expect(screen.getByText("One step left")).toBeTruthy();
    expect(
      screen.getByText(/Add the API key Linear needs.*under Custom Headers/),
    ).toBeTruthy();
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

  it.each(["", "?setup=other"])(
    "shows nothing without a known setup step (%s)",
    (search) => {
      renderAt(search);

      expect(screen.queryByText("One step left")).toBeNull();
      expect(screen.queryByText(/ready to use/)).toBeNull();
    },
  );
});
