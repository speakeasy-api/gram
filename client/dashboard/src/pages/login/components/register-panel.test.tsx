import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { RegisterPanel } from "./register-panel";

const locationReplace = vi.fn();

const mocks = vi.hoisted(() => ({
  buildRegisterMutation: vi.fn(),
  useGramContext: vi.fn(),
  useMutation: vi.fn(),
  useTelemetry: vi.fn(),
}));

vi.mock("@/contexts/Telemetry", () => ({ useTelemetry: mocks.useTelemetry }));
vi.mock("@gram/client/funcs/authInfo", () => ({ authInfo: vi.fn() }));
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: mocks.useGramContext,
}));
vi.mock("@gram/client/react-query/register.js", () => ({
  buildRegisterMutation: mocks.buildRegisterMutation,
}));
vi.mock("@tanstack/react-query", () => ({ useMutation: mocks.useMutation }));

beforeEach(() => {
  mocks.useTelemetry.mockReturnValue({ capture: vi.fn() });
  mocks.useGramContext.mockReturnValue({});
  mocks.useMutation.mockImplementation(({ onSuccess }) => ({
    error: null,
    isPending: false,
    mutate: () => onSuccess(),
  }));
  vi.stubGlobal("location", {
    origin: "https://app.example",
    replace: locationReplace,
  });
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  vi.unstubAllGlobals();
});

describe("RegisterPanel", () => {
  it("returns to the preserved destination after organization creation", async () => {
    const user = userEvent.setup();
    render(
      <MemoryRouter>
        <RegisterPanel redirectTo="/cli/callback" />
      </MemoryRouter>,
    );

    await user.type(screen.getByLabelText("Company name"), "Acme Inc");
    await user.click(
      screen.getByRole("button", { name: /create organization/i }),
    );

    expect(locationReplace).toHaveBeenCalledWith(
      "https://app.example/cli/callback",
    );
  });

  it("returns to the dashboard root when no destination was preserved", async () => {
    const user = userEvent.setup();
    render(
      <MemoryRouter>
        <RegisterPanel />
      </MemoryRouter>,
    );

    await user.type(screen.getByLabelText("Company name"), "Acme Inc");
    await user.click(
      screen.getByRole("button", { name: /create organization/i }),
    );

    expect(locationReplace).toHaveBeenCalledWith("/");
  });
});
