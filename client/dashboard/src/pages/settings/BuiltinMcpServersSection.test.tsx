import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const state = vi.hoisted(() => ({
  isAdmin: true,
  hidden: false,
  isError: false,
  save: vi.fn(),
  refetch: vi.fn(),
}));
vi.mock("@/components/mcp/useBuiltinMcpServers", () => ({
  useBuiltinMcpServers: () => ({
    isAdmin: state.isAdmin,
    isError: state.isError,
    isLoading: false,
    refetch: state.refetch,
    servers: [
      {
        id: "builtin-one",
        key: "slack",
        name: "Slack",
        hidden: state.hidden,
        environmentId: "environment-one",
        version: "3",
      },
    ],
  }),
}));
vi.mock("@/components/mcp/useUpdateBuiltinMcpServer", () => ({
  useUpdateBuiltinMcpServer: () => ({ save: state.save, isPending: false }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: { builtin: { href: (key: string) => `/mcp/builtin/${key}` } },
  }),
}));
vi.mock("react-router", () => ({
  Link: ({
    to,
    children,
    ...rest
  }: {
    to: string;
    children: React.ReactNode;
  }) => (
    <a href={to} {...rest}>
      {children}
    </a>
  ),
}));

import { BuiltinMcpServersSection } from "./BuiltinMcpServersSection";

afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  state.isAdmin = true;
  state.hidden = false;
  state.isError = false;
});

describe("built-in project settings", () => {
  it("hides the section from non-admins", () => {
    state.isAdmin = false;
    render(<BuiltinMcpServersSection />);
    expect(screen.queryByText("Built-in MCP servers")).toBeNull();
  });
  it("hides visually while preserving environment and version", () => {
    const { rerender } = render(<BuiltinMcpServersSection />);
    expect(screen.getByRole("switch").getAttribute("aria-checked")).toBe(
      "true",
    );
    fireEvent.click(
      screen.getByRole("switch", { name: "Show Slack in MCP Servers" }),
    );
    expect(state.save).toHaveBeenCalledWith({
      key: "slack",
      hidden: true,
      environmentId: "environment-one",
      expectedVersion: "3",
    });
    state.hidden = true;
    rerender(<BuiltinMcpServersSection />);
    expect(screen.getByRole("switch").getAttribute("aria-checked")).toBe(
      "false",
    );
  });
  it("keeps hidden entries configurable and lets admins unhide them", () => {
    state.hidden = true;
    render(<BuiltinMcpServersSection />);
    expect(
      screen.getByRole("link", { name: "Slack" }).getAttribute("href"),
    ).toBe("/mcp/builtin/slack");
    const toggle = screen.getByRole("switch", {
      name: "Show Slack in MCP Servers",
    });
    expect(toggle.getAttribute("aria-checked")).toBe("false");
    fireEvent.click(toggle);
    expect(state.save).toHaveBeenCalledWith(
      expect.objectContaining({ hidden: false }),
    );
  });
  it("shows a recoverable error instead of stale controls", () => {
    state.isError = true;
    render(<BuiltinMcpServersSection />);
    expect(screen.queryByRole("switch")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(state.refetch).toHaveBeenCalled();
  });
});
