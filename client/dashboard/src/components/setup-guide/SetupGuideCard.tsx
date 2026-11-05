import { SetupGuideAction } from "./SetupGuideLink";
import { useSetupGuide } from "@/components/setup-guide/useSetupGuide";
import { Text } from "@/components/ui/Text";
/**
 * The sidebar counterpart to {@link SetupGuideCallout}: same lookup, same
 * panel, sized for a nav card instead of a page banner.
 *
 * Renders nothing when no guide resolves, which is the common case, so callers
 * can drop it in without a guard.
 */
export function SetupGuideCard({
  registrySpecifier,
  serverUrl,
}: {
  registrySpecifier?: string;
  serverUrl?: string;
}): React.JSX.Element | null {
  const guide = useSetupGuide({ registrySpecifier, serverUrl });

  // A card with nothing to click is just a sentence about work you cannot
  // start: on mobile the panel is unavailable, and two matched guides have no
  // single docs page to fall back to.
  if (!guide || (!guide.openGuide && !guide.only)) return null;

  return (
    <div className="bg-card border-border dark:bg-neutral-950 flex flex-col gap-2 border px-4 py-3 shadow-md">
      <Text variant="small" muted className="text-xs">
        This MCP server may require some additional setup.
      </Text>
      <SetupGuideAction guide={guide} />
    </div>
  );
}
