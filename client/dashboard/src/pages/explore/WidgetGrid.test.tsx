import type { WidgetPreset } from "@gram/client/models/components/widgetpreset.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { WidgetGrid } from "./WidgetGrid";

const testState = vi.hoisted(() => ({ staff: false }));

vi.mock("@/contexts/Auth", () => ({
  useIsSpeakeasyStaff: () => testState.staff,
}));
// The grid's job is placement; each card's answer is WidgetView's, tested
// on its own.
vi.mock("./WidgetView", () => ({
  WidgetView: ({
    widget,
    className,
    footer,
  }: {
    widget: { name: string };
    className?: string;
    footer?: ReactNode;
  }) => (
    <section aria-label={widget.name} className={className}>
      {footer}
    </section>
  ),
}));

const number = { type: "number", options: {} };
const count = { window: "24h", grain: "none", measures: [] };

const preset: WidgetPreset = {
  page: "home",
  rows: [
    {
      widgets: [
        {
          key: "a",
          name: "A",
          dataset: "sessions",
          span: 3,
          query: count,
          visualization: number,
        },
        {
          key: "b",
          name: "B",
          dataset: "sessions",
          span: 3,
          query: count,
          visualization: number,
        },
        {
          key: "c",
          name: "C",
          dataset: "sessions",
          span: 6,
          query: count,
          visualization: number,
        },
      ],
    },
    {
      widgets: [
        {
          key: "d",
          name: "D",
          dataset: "sessions",
          span: 4,
          query: count,
          visualization: number,
        },
        {
          key: "e",
          name: "E",
          dataset: "sessions",
          span: 12,
          query: count,
          visualization: number,
        },
      ],
    },
  ],
};

describe("WidgetGrid", () => {
  beforeEach(() => {
    testState.staff = false;
    localStorage.clear();
  });
  afterEach(() => {
    cleanup();
  });

  it("places each widget by its span, quarters pairing up below lg", () => {
    render(<WidgetGrid preset={preset} />);
    const classOf = (name: string) =>
      screen.getByRole("region", { name }).className;
    expect(classOf("A")).toBe("col-span-6 lg:col-span-3");
    expect(classOf("C")).toBe("col-span-12 lg:col-span-6");
    expect(classOf("D")).toBe("col-span-12 lg:col-span-4");
    expect(classOf("E")).toBe("col-span-12");
  });

  it("keeps rows in order, left to right", () => {
    render(<WidgetGrid preset={preset} />);
    expect(
      screen
        .getAllByRole("region")
        .map((card) => card.getAttribute("aria-label")),
    ).toEqual(["A", "B", "C", "D", "E"]);
  });

  it("offers staff the legacy figures beside the widgets", () => {
    testState.staff = true;
    render(<WidgetGrid preset={preset} legacy={{ a: "1,204" }} />);
    expect(screen.queryByText("1,204")).toBeNull();

    fireEvent.click(
      screen.getByRole("switch", { name: "Compare with legacy" }),
    );
    expect(screen.getByText("1,204")).toBeTruthy();
    expect(localStorage.getItem("gram-widget-compare-legacy")).toBe("1");
  });

  it("offers no comparison with nothing to compare", () => {
    testState.staff = true;
    render(<WidgetGrid preset={preset} />);
    expect(screen.queryByRole("switch")).toBeNull();
  });
});
