/**
 * Up to two uppercase initials for an avatar fallback. Names arrive from
 * directory sync and from client-supplied session subjects, so this has to
 * tolerate a single word, extra whitespace, and an empty string.
 */
export function getInitials(name: string): string {
  return name
    .split(" ")
    .map((part) => part[0])
    .join("")
    .slice(0, 2)
    .toUpperCase();
}
