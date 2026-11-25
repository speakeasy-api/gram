import type { Widget } from "@gram/client/models/components/widget.js";
import { act, cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { WidgetCards } from "./WidgetCards";

// Each card's answer is WidgetView's, tested on its own; here a card only
// has to exist once it is mounted.
vi.mock("./WidgetView", () => ({
  WidgetView: ({ widget }: { widget: { name: string } }) => (
    <section aria-label={widget.name} />
  ),
  WidgetPlaceholder: ({ chartType }: { chartType: string }) => (
    <div data-testid="placeholder" data-chart-type={chartType} />
  ),
}));

/** An observer the test reports intersections through, by hand. */
const observers: {
  callback: IntersectionObserverCallback;
  target?: Element;
}[] = [];
class ManualObserver {
  private readonly entry: (typeof observers)[number];
  constructor(callback: IntersectionObserverCallback) {
    this.entry = { callback };
    observers.push(this.entry);
  }
  observe(target: Element) {
    this.entry.target = target;
  }
  disconnect() {}
  unobserve() {}
  takeRecords() {
    return [];
  }
}
vi.stubGlobal("IntersectionObserver", ManualObserver);

function widget(id: string, name: string, chartType = "number"): Widget {
  return {
    id,
    name,
    dataset: "sessions",
    query: {},
    visualization: { type: chartType },
    dashboards: [],
    projectId: "project",
    organizationId: "org",
    createdAt: new Date(),
    updatedAt: new Date(),
  };
}

describe("WidgetCards", () => {
  afterEach(() => {
    cleanup();
    observers.length = 0;
  });

  it("mounts a card, and so runs its query, only once it scrolls into view", () => {
    render(
      <WidgetCards
        widgets={[widget("w-1", "First"), widget("w-2", "Second")]}
        page={{}}
        actionsFor={() => []}
        onOpen={() => {}}
      />,
    );
    expect(screen.queryByRole("region")).toBeNull();

    const first = observers[0]!;
    act(() =>
      first.callback(
        [
          {
            isIntersecting: true,
            target: first.target!,
          } as IntersectionObserverEntry,
        ],
        first as unknown as IntersectionObserver,
      ),
    );
    expect(screen.getByRole("region", { name: "First" })).toBeTruthy();
    expect(screen.queryByRole("region", { name: "Second" })).toBeNull();
  });

  it("says so when no widget matches the filters", () => {
    render(
      <WidgetCards
        widgets={[]}
        page={{}}
        actionsFor={() => []}
        onOpen={() => {}}
      />,
    );
    expect(screen.getByText("No widgets match these filters.")).toBeTruthy();
  });

  it("holds each card's place at the size its chart type draws at", () => {
    render(
      <WidgetCards
        widgets={[widget("w-1", "Tile"), widget("w-2", "Chart", "line")]}
        page={{}}
        actionsFor={() => []}
        onOpen={() => {}}
      />,
    );
    expect(
      screen
        .getAllByTestId("placeholder")
        .map((placeholder) => placeholder.dataset.chartType),
    ).toEqual(["number", "line"]);
  });
});
