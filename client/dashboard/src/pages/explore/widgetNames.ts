/** At most this long, matching the widgets service. */
export const MAX_NAME_LENGTH = 200;

const COPY_SUFFIX = " (copy)";

/**
 * The name a copy of a widget is offered, shortened to fit as the widgets
 * service shortens a duplicate's: by code points, not UTF-16 units.
 */
export function copyName(name: string): string {
  const room = MAX_NAME_LENGTH - Array.from(COPY_SUFFIX).length;
  return Array.from(name).slice(0, room).join("") + COPY_SUFFIX;
}
