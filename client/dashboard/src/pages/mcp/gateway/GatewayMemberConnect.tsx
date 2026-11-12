/**
 * The inline Connect action for a gateway member that needs the viewer's own
 * account. Overview's Status column and Inspect's notice both use it, so the
 * action reads the same on each page.
 */
export function GatewayMemberConnect({
  name,
  onConnect,
}: {
  name: string;
  onConnect: () => void;
}): JSX.Element {
  return (
    <button
      type="button"
      onClick={onConnect}
      aria-label={`Connect your account for ${name}`}
      className="text-foreground text-sm underline underline-offset-2"
    >
      Connect
    </button>
  );
}
