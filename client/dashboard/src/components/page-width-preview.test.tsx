import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { PageWidthPreview } from "./page-width-preview";

const PAGE_WIDTH_PREVIEW_STORAGE_KEY = "gram:page-width-preview";

afterEach(() => {
  cleanup();
  localStorage.clear();
  delete document.documentElement.dataset.pageWidth;
  window.history.replaceState(null, "", "/");
});

describe("PageWidthPreview", () => {
  it("renders nothing unless turned on", () => {
    const { container } = render(<PageWidthPreview />);
    expect(container.firstChild).toBeNull();
    expect(document.documentElement.dataset.pageWidth).toBeUndefined();
  });

  it("turns on from the query param and sets the root attribute", () => {
    window.history.replaceState(null, "", "/?pageWidth=1600");
    render(<PageWidthPreview />);
    expect(document.documentElement.dataset.pageWidth).toBe("1600");
    expect(localStorage.getItem(PAGE_WIDTH_PREVIEW_STORAGE_KEY)).toBe("1600");
  });

  it("switches width and clears everything on close", () => {
    localStorage.setItem(PAGE_WIDTH_PREVIEW_STORAGE_KEY, "1440");
    render(<PageWidthPreview />);
    act(() => fireEvent.click(screen.getByRole("button", { name: "Full" })));
    expect(document.documentElement.dataset.pageWidth).toBe("full");

    act(() =>
      fireEvent.click(
        screen.getByRole("button", { name: "Close page width preview" }),
      ),
    );
    expect(document.documentElement.dataset.pageWidth).toBeUndefined();
    expect(localStorage.getItem(PAGE_WIDTH_PREVIEW_STORAGE_KEY)).toBeNull();
    expect(screen.queryByRole("group")).toBeNull();
  });

  it("ignores unknown values", () => {
    window.history.replaceState(null, "", "/?pageWidth=999");
    const { container } = render(<PageWidthPreview />);
    expect(container.firstChild).toBeNull();
    expect(document.documentElement.dataset.pageWidth).toBeUndefined();
  });
});
