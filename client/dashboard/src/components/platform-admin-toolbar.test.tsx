import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("@/contexts/Auth", () => ({
  useIsPlatformAdmin: () => true,
  useOrganization: () => ({
    id: "org_<ORG_ID>",
    slug: "test-org",
    projects: [],
  }),
  useSession: () => ({ session: {} }),
}));

vi.mock("@gram/client/react-query/listToolsetsForOrg.js", () => ({
  useListToolsetsForOrg: () => ({ data: { toolsets: [] } }),
}));

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
}));

vi.mock("./platform-admin-panel", () => ({
  PlatformAdminFeaturesPanel: () => <div>Features panel</div>,
  PlatformAdminInfoPanel: () => <div>Info panel</div>,
  PlatformAdminOnboardingPanel: () => <div>Onboarding panel</div>,
}));

vi.mock("@/components/ui/Switch", () => ({
  Switch: () => <button type="button">Switch</button>,
}));

import { PlatformAdminToolbar } from "./platform-admin-toolbar";

const initialWindowWidth = window.innerWidth;

afterEach(() => {
  cleanup();
  localStorage.clear();
  Object.defineProperty(window, "innerWidth", {
    configurable: true,
    value: initialWindowWidth,
  });
});

describe("PlatformAdminToolbar", () => {
  it("uses a wider panel without disabling text selection", () => {
    render(<PlatformAdminToolbar />);

    const title = screen.getByText("Developer Toolkit");
    const panel = title.closest("[class~='w-96']");
    const toolbar = title.closest("[class~='fixed']");

    expect(panel).not.toBeNull();
    expect(toolbar?.classList.contains("select-none")).toBe(false);
  });

  it("restores a valid dragged position on a narrow viewport", () => {
    Object.defineProperty(window, "innerWidth", {
      configurable: true,
      value: 360,
    });
    localStorage.setItem(
      "gram-rbac-dev-toolbar-pos",
      JSON.stringify({ x: 24, y: 10 }),
    );

    render(<PlatformAdminToolbar />);

    const toolbar = screen
      .getByText("Developer Toolkit")
      .closest<HTMLElement>("[class~='fixed']");
    expect(toolbar?.style.left).toBe("24px");
  });

  it("expands from the caret after the toolbar has been dragged", () => {
    render(<PlatformAdminToolbar />);

    const dragHandle = screen.getByRole("button", {
      name: /Developer Toolkit/,
    });
    fireEvent.pointerDown(dragHandle, {
      button: 0,
      clientX: 0,
      clientY: 0,
    });
    fireEvent.pointerMove(window, { clientX: 10, clientY: 0 });
    fireEvent.pointerUp(window);

    fireEvent.click(
      screen.getByRole("button", { name: "Expand developer toolkit" }),
    );

    expect(screen.getByText("Info panel")).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "Collapse developer toolkit" }),
    ).toBeTruthy();
  });
});
