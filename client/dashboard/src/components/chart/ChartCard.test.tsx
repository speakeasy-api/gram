import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ChartCard } from "./ChartCard";

afterEach(cleanup);

const defaultProps = {
  title: "Server Usage",
  chartId: "server-usage",
  expandedChart: null,
  onExpand: vi.fn(),
};

describe("ChartCard", () => {
  it("renders the title and children", () => {
    render(
      <ChartCard {...defaultProps}>
        <span>chart content</span>
      </ChartCard>,
    );
    expect(screen.getByText("Server Usage")).toBeTruthy();
    expect(screen.getByText("chart content")).toBeTruthy();
  });

  it("shows an expand button when there is data and the chart is not expanded", () => {
    render(<ChartCard {...defaultProps}>-</ChartCard>);
    expect(screen.getByRole("button", { name: "Expand chart" })).toBeTruthy();
  });

  it("calls onExpand with chartId when the expand button is clicked", () => {
    const onExpand = vi.fn();
    render(
      <ChartCard {...defaultProps} onExpand={onExpand}>
        -
      </ChartCard>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Expand chart" }));
    expect(onExpand).toHaveBeenCalledWith("server-usage");
  });

  it("shows a minimize button and calls onExpand(null) when the chart is expanded", () => {
    const onExpand = vi.fn();
    render(
      <ChartCard
        {...defaultProps}
        expandedChart="server-usage"
        onExpand={onExpand}
      >
        -
      </ChartCard>,
    );
    const btn = screen.getByRole("button", { name: "Minimize chart" });
    expect(btn).toBeTruthy();
    fireEvent.click(btn);
    expect(onExpand).toHaveBeenCalledWith(null);
  });

  it("hides the expand button when hasData is false", () => {
    render(
      <ChartCard {...defaultProps} hasData={false}>
        -
      </ChartCard>,
    );
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("still shows a minimize button when hasData is false but the chart is expanded", () => {
    render(
      <ChartCard {...defaultProps} expandedChart="server-usage" hasData={false}>
        -
      </ChartCard>,
    );
    expect(screen.getByRole("button", { name: "Minimize chart" })).toBeTruthy();
  });

  it("hides the card when a different chart is expanded", () => {
    const { container } = render(
      <ChartCard {...defaultProps} expandedChart="other-chart">
        -
      </ChartCard>,
    );
    expect(container.firstElementChild?.classList.contains("hidden")).toBe(
      true,
    );
  });

  it("remains visible when it is the expanded chart", () => {
    const { container } = render(
      <ChartCard {...defaultProps} expandedChart="server-usage">
        -
      </ChartCard>,
    );
    expect(container.firstElementChild?.classList.contains("hidden")).toBe(
      false,
    );
  });

  it("remains visible when no chart is expanded", () => {
    const { container } = render(
      <ChartCard {...defaultProps} expandedChart={null}>
        -
      </ChartCard>,
    );
    expect(container.firstElementChild?.classList.contains("hidden")).toBe(
      false,
    );
  });
});
