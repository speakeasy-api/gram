/**
 * Scrolls `element` to the top of the page's own scroll container, leaving
 * every other ancestor where it is.
 *
 * `scrollIntoView` scrolls each scrollable ancestor, including ones clipped
 * with `overflow: hidden`, so on a page whose header is pinned above a
 * scrolling body it would push the header out of view. Pages mark their
 * scroll container with `data-page-bound`; without one this falls back to
 * `scrollIntoView`, and a bound pane that doesn't overflow is left alone.
 */
export function scrollWithinPage(
  element: HTMLElement,
  {
    offset = 32,
    behavior = "smooth",
  }: { offset?: number; behavior?: ScrollBehavior } = {},
): void {
  const scroller =
    element.parentElement?.closest<HTMLElement>("[data-page-bound]");
  if (!scroller) {
    element.scrollIntoView({ behavior, block: "start" });
    return;
  }
  // A bound pane with nothing to scroll already shows the element; scrolling
  // any further out would move the pinned header.
  if (scroller.scrollHeight <= scroller.clientHeight) return;
  // A sticky strip at the scroller's top (the detail frame's tabs) sets
  // scroll-padding-top, so the element lands below it rather than under it.
  const padding = parseFloat(getComputedStyle(scroller).scrollPaddingTop) || 0;
  const top =
    element.getBoundingClientRect().top -
    scroller.getBoundingClientRect().top +
    scroller.scrollTop -
    offset -
    padding;
  scroller.scrollTo({ top: Math.max(0, top), behavior });
}
