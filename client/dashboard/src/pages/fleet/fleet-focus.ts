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
