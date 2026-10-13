import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ShadowAITargetsTab } from "./ShadowAITargetsTab";

// An org admin: the gate lets the body through. The mock records what the
// gate was asked for so the test can pin the scope.
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({
    scope,
    level,
    children,
  }: {
    scope: string;
    level: string;
    children: ReactNode;
  }) => (
    <div data-testid="gate" data-scope={scope} data-level={level}>
      {children}
    </div>
  ),
}));

vi.mock("@/pages/device-agent/ai-scan-targets", () => ({
  AiScanTargetsSection: ({ title }: { title?: string }) => (
    <div data-testid="scan-targets">{title}</div>
  ),
}));

afterEach(cleanup);

describe("ShadowAITargetsTab", () => {
  it("renders the organization's scan target library for an org admin, retitled for the tab", () => {
    render(<ShadowAITargetsTab />);

    expect(screen.getByTestId("scan-targets").textContent).toBe(
      "Shadow AI targets",
    );
  });

  it("gates the library behind org:admin with the page-level notice", () => {
    render(<ShadowAITargetsTab />);

    const gate = screen.getByTestId("gate");
    expect(gate.getAttribute("data-scope")).toBe("org:admin");
    expect(gate.getAttribute("data-level")).toBe("page");
  });
});
