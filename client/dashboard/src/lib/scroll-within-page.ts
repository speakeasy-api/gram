/**
 * Scrolls `element` to the top of the page's own scroll container, leaving
 * every other ancestor where it is.
 *
 * `scrollIntoView` scrolls each scrollable ancestor, including ones clipped
 * with `overflow: hidden`, so on a page whose header is pinned above a
 * scrolling body it would push the header out of view. Pages mark their
 * scroll container with `data-page-bound`; without one this falls back to
 * `scrollIntoView`.
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
  if (!scroller || scroller.scrollHeight <= scroller.clientHeight) {
    element.scrollIntoView({ behavior, block: "start" });
    return;
  }
  const top =
    element.getBoundingClientRect().top -
    scroller.getBoundingClientRect().top +
    scroller.scrollTop -
    offset;
  scroller.scrollTo({ top: Math.max(0, top), behavior });
}
