/** Hook events Claude Cowork reports, under either of its source names. */
export function isCoworkSource(source: string): boolean {
  return source === "cowork" || source === "claude-cowork";
}

/** Everything the device agent or manual hooks cover: not Cowork. */
export function isNotCoworkSource(source: string): boolean {
  return !isCoworkSource(source);
}
