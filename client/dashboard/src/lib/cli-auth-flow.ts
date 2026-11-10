/**
 * Whether a location is the CLI login hand-off
 * ("/?from_cli=true&cli_callback_url=…"), which returns a key to the CLI's
 * local callback.
 */
export function isCliAuthFlowLocation(
  pathname: string,
  search: string,
): boolean {
  if (pathname !== "/") return false;
  const params = new URLSearchParams(search);
  return (
    params.get("from_cli") === "true" && Boolean(params.get("cli_callback_url"))
  );
}
