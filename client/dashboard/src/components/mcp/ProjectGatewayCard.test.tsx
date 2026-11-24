import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ProjectGatewayCard } from "./ProjectGatewayCard";

const { goTo } = vi.hoisted(() => ({
  goTo: vi.fn(),
}));

vi.mock("@/components/icon-dither", () => ({
  SPEAKEASY_RAINBOW: ["#000000"],
  useIconDither: () => ({
    canvasRef: { current: null },
    start: vi.fn(),
    stop: vi.fn(),
  }),
}));

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: {
      projectGateway: {
        goTo,
      },
    },
  }),
}));

afterEach(cleanup);
beforeEach(() => {
  goTo.mockReset();
});

describe("ProjectGatewayCard", () => {
  it("renders with the rainbow frame and no warning badge when available", () => {
    const { container } = render(<ProjectGatewayCard status="available" />);

    expect(screen.getByText("Project Gateway")).toBeTruthy();
    expect(screen.queryByText("Off")).toBeNull();
    expect(screen.queryByText("Unavailable")).toBeNull();
    expect(container.querySelector(".speakeasy-rainbow-frame")).toBeTruthy();
  });

  it("renders in a disabled visual state with an Off badge when off and stays clickable", () => {
    const { container } = render(<ProjectGatewayCard status="off" />);

    expect(screen.getByText("Project Gateway")).toBeTruthy();
    expect(screen.getByText("Off")).toBeTruthy();
    expect(container.querySelector(".speakeasy-rainbow-frame")).toBeNull();

    fireEvent.click(screen.getByText("Project Gateway"));
    expect(goTo).toHaveBeenCalledTimes(1);
  });

  it("renders in a disabled visual state with an Unavailable badge when unavailable and stays clickable", () => {
    const { container } = render(<ProjectGatewayCard status="unavailable" />);

    expect(screen.getByText("Project Gateway")).toBeTruthy();
    expect(screen.getByText("Unavailable")).toBeTruthy();
    expect(container.querySelector(".speakeasy-rainbow-frame")).toBeNull();

    fireEvent.click(screen.getByText("Project Gateway"));
    expect(goTo).toHaveBeenCalledTimes(1);
  });

  it("omits status badges while loading", () => {
    const { container } = render(<ProjectGatewayCard status="loading" />);

    expect(screen.getByText("Project Gateway")).toBeTruthy();
    expect(screen.queryByText("Off")).toBeNull();
    expect(screen.queryByText("Unavailable")).toBeNull();
    expect(container.querySelector(".speakeasy-rainbow-frame")).toBeNull();
  });
});
