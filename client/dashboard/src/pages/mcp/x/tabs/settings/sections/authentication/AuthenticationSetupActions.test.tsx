import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { AuthenticationSetupActions } from "./AuthenticationSetupActions";

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => children,
}));

afterEach(cleanup);

const defaultProps = {
  onStartManual: vi.fn(),
};

describe("AuthenticationSetupActions", () => {
  it("preserves manual setup for standard targets", () => {
    render(<AuthenticationSetupActions {...defaultProps} />);

    expect(
      screen.getByText("OAuth metadata was not advertised by this server."),
    ).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Use Discovered" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Configure Manually" }));
    expect(defaultProps.onStartManual).toHaveBeenCalledOnce();
  });
});
