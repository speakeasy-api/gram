import { render, screen, fireEvent } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { PublicMcpWarningDialog } from "./public-mcp-warning-dialog";

// moonshine's bundle imports lucide-react/dynamicIconImports which can't be
// resolved in the test environment (no package exports map). Mock the whole
// package so Button renders as a plain <button>.
vi.mock("@speakeasy-api/moonshine", () => ({
  Button: ({
    children,
    onClick,
    disabled,
  }: {
    children: React.ReactNode;
    onClick?: () => void;
    disabled?: boolean;
    variant?: string;
  }) => (
    <button onClick={onClick} disabled={disabled}>
      {children}
    </button>
  ),
}));

const defaultProps = {
  isOpen: true,
  onClose: vi.fn(),
  onConfirm: vi.fn(),
  environmentName: "Production",
  environmentSlug: "production",
  variableNames: ["STRIPE_API_KEY", "DATABASE_URL"],
};

describe("PublicMcpWarningDialog", () => {
  it("renders title, body, variable names, and the environment link", () => {
    render(<PublicMcpWarningDialog {...defaultProps} />);

    expect(
      screen.getByText("Share system secrets with public callers."),
    ).toBeTruthy();
    // "Production" appears both in the body and in the link label; use getAllByText.
    expect(screen.getAllByText(/Production/).length).toBeGreaterThan(0);
    expect(screen.getByText("STRIPE_API_KEY")).toBeTruthy();
    expect(screen.getByText("DATABASE_URL")).toBeTruthy();

    const link = screen.getByRole("link", { name: /Review in Production/ });
    expect(link.getAttribute("href")).toBe("/environments/production");
    expect(link.getAttribute("target")).toBe("_blank");
  });

  it("fires onConfirm when the destructive action is clicked", () => {
    const onConfirm = vi.fn();
    render(<PublicMcpWarningDialog {...defaultProps} onConfirm={onConfirm} />);
    fireEvent.click(screen.getByRole("button", { name: /Make public anyway/ }));
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  it("fires onClose when Cancel is clicked", () => {
    const onClose = vi.fn();
    render(<PublicMcpWarningDialog {...defaultProps} onClose={onClose} />);
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
