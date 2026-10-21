/** Called after the sheet releases its focus trap, or after a desktop close. */
export function restoreFleetFocus(id: string | null): void {
  const row = id
    ? document.querySelector<HTMLButtonElement>(
        `[data-fleet-row="${CSS.escape(id)}"]`,
      )
    : null;
  (row ?? document.getElementById("fleet-collection-heading"))?.focus({
    preventScroll: true,
  });
}

/** Aging out should repair removed inspector focus without stealing toolbar focus. */
export function restoreFleetFocusAfterRemoval(id: string): void {
  const active = document.activeElement;
  if (
    active === document.body ||
    active?.closest(".fleet-inspector, [data-fleet-inspector]")
  ) {
    restoreFleetFocus(id);
  }
}
