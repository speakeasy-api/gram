import { afterEach, describe, expect, it, vi } from "vitest";
import { scrollWithinPage } from "./scroll-within-page";

function mount(html: string): HTMLElement {
  document.body.innerHTML = html;
  return document.getElementById("target")!;
}

function sizeScroller(scroller: HTMLElement, scroll: number, client: number) {
  Object.defineProperty(scroller, "scrollHeight", { value: scroll });
  Object.defineProperty(scroller, "clientHeight", { value: client });
}

describe("scrollWithinPage", () => {
  afterEach(() => {
    document.body.innerHTML = "";
  });

  it("falls back to scrollIntoView without a bound pane", () => {
    const target = mount(`<div><section id="target"></section></div>`);
    const scrollIntoView = vi.fn<HTMLElement["scrollIntoView"]>();
    target.scrollIntoView = scrollIntoView;

    scrollWithinPage(target);

    expect(scrollIntoView).toHaveBeenCalledOnce();
  });

  it("does nothing when the bound pane doesn't overflow", () => {
    const target = mount(
      `<div data-page-bound id="pane"><section id="target"></section></div>`,
    );
    const pane = document.getElementById("pane")!;
    sizeScroller(pane, 400, 400);
    const scrollIntoView = vi.fn<HTMLElement["scrollIntoView"]>();
    target.scrollIntoView = scrollIntoView;
    const scrollTo = vi.spyOn(pane, "scrollTo").mockImplementation(() => {});

    scrollWithinPage(target);

    expect(scrollIntoView).not.toHaveBeenCalled();
    expect(scrollTo).not.toHaveBeenCalled();
  });

  it("scrolls only the bound pane when it overflows", () => {
    const target = mount(
      `<div data-page-bound id="pane"><section id="target"></section></div>`,
    );
    const pane = document.getElementById("pane")!;
    sizeScroller(pane, 1200, 400);
    const scrollIntoView = vi.fn<HTMLElement["scrollIntoView"]>();
    target.scrollIntoView = scrollIntoView;
    const scrollTo = vi.spyOn(pane, "scrollTo").mockImplementation(() => {});

    vi.spyOn(pane, "getBoundingClientRect").mockReturnValue({
      top: 100,
    } as DOMRect);
    vi.spyOn(target, "getBoundingClientRect").mockReturnValue({
      top: 700,
    } as DOMRect);
    Object.defineProperty(pane, "scrollTop", { value: 250 });

    scrollWithinPage(target, { behavior: "auto" });

    expect(scrollIntoView).not.toHaveBeenCalled();
    // 700 - 100 + 250 - 32 (default offset)
    expect(scrollTo).toHaveBeenCalledWith({ top: 818, behavior: "auto" });
  });
});
