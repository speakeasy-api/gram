import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SplitRankedBarList } from "./SplitRankedBarList";

afterEach(cleanup);

describe("SplitRankedBarList", () => {
  it("names the red share as failed calls unless told otherwise", () => {
    render(
      <SplitRankedBarList
        items={[{ key: "a", label: "search_logs", value: 4, failed: 3 }]}
      />,
    );

    expect(screen.getByTitle("3 failed").textContent).toContain("3 fail");
  });

  it("uses the caller's word for the red share", () => {
    render(
      <SplitRankedBarList
        failedLabel="blocked"
        failedTitle="blocked"
        items={[{ key: "a", label: "search_logs", value: 4, failed: 3 }]}
      />,
    );

    expect(screen.getByTitle("3 blocked").textContent).toContain("3 blocked");
    expect(screen.queryByTitle("3 failed")).toBeNull();
  });

  it("says nothing beside an item with no red share", () => {
    render(
      <SplitRankedBarList
        failedLabel="blocked"
        failedTitle="blocked"
        items={[{ key: "a", label: "search_logs", value: 4, failed: 0 }]}
      />,
    );

    expect(screen.queryByText(/blocked/)).toBeNull();
  });

  it("makes the label a button only for items that can be selected", () => {
    const onSelect = vi.fn<() => void>();
    render(
      <SplitRankedBarList
        items={[
          { key: "a", label: "search_logs", value: 4, failed: 1, onSelect },
          { key: "b", label: "query_db", value: 2, failed: 0 },
        ]}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "search_logs" }));

    expect(onSelect).toHaveBeenCalledOnce();
    expect(screen.queryByRole("button", { name: "query_db" })).toBeNull();
  });
});
