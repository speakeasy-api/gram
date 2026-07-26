import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { AuthenticationSetupActions } from "./AuthenticationSetupActions";

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => children,
}));

vi.mock("@speakeasy-api/moonshine", () => {
  const Button = ({ children }: { children: ReactNode }) => (
    <button>{children}</button>
  );
  Button.Text = ({ children }: { children: ReactNode }) => children;
  return { Button };
});

afterEach(cleanup);

const defaultProps = {
  hasDiscoveredAuthorizationServer: false,
  onUseDiscovered: vi.fn(),
  onStartManual: vi.fn(),
};

describe("AuthenticationSetupActions", () => {
  it("explains unavailable discovery without rendering a disabled button", () => {
    render(
      <AuthenticationSetupActions
        probeStatus="unavailable"
        {...defaultProps}
      />,
    );

    expect(
      screen.getByText("OAuth metadata was not advertised by this server."),
    ).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Use Discovered" })).toBeNull();
  });

  it("shows subtle progress copy while discovery is running", () => {
    render(
      <AuthenticationSetupActions probeStatus="loading" {...defaultProps} />,
    );

    expect(
      screen.getByText("Checking for advertised OAuth metadata…"),
    ).toBeTruthy();
  });

  it("renders the discovery action when metadata is available", () => {
    render(
      <AuthenticationSetupActions
        probeStatus="available"
        {...defaultProps}
        hasDiscoveredAuthorizationServer
      />,
    );

    expect(screen.getByRole("button", { name: "Use Discovered" })).toBeTruthy();
  });
});
