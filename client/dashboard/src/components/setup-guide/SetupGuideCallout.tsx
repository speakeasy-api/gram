import { docsUrl } from "@/components/setup-guide/guideDocs";
import { useSetupGuide } from "@/components/setup-guide/useSetupGuide";
import { StatusBanner } from "@/components/status-banner";
import { Text } from "@/components/ui/Text";
import type { MCPSetupGuide } from "@gram/client/models/components/mcpsetupguide.js";
import { Button } from "@/components/ui/Button";
import { BookOpen, ExternalLink } from "lucide-react";

/**
 * Surfaces the published setup guide for an upstream MCP server, when one
 * exists, as a short note plus a side panel holding the full instructions.
 *
 * Renders nothing when no guide resolves, which is the common case, so callers
 * can drop it in without a guard.
 */
export function SetupGuideCallout({
  registrySpecifier,
  serverUrl,
  iconUrl,
}: {
  registrySpecifier?: string;
  serverUrl?: string;
  /** The server's icon, which the panel wears in its header. */
  iconUrl?: string;
}): React.JSX.Element | null {
  const guide = useSetupGuide({ registrySpecifier, serverUrl, iconUrl });

  // A banner with nothing to click is just a sentence about work you cannot
  // start: on mobile the panel is unavailable, and two matched guides have no
  // single docs page to fall back to.
  if (!guide || (!guide.openGuide && !guide.only)) return null;

  return (
    <StatusBanner tone="information" className="mb-8">
      {/* Copy left, actions right on one row: the banner is a short aside, and
          stacking the buttons under it gave it the height of a page section. */}
      <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-3 px-5 py-3.5">
        <div className="flex min-w-0 flex-col gap-1">
          <div className="flex items-center gap-2">
            <BookOpen className="text-default-information h-4 w-4 shrink-0" />
            <Text className="text-default-information text-sm font-semibold">
              Setup guide available
            </Text>
          </div>
          <Text variant="small" className="text-muted-foreground/90">
            {describeSetupWork(guide.only)}
          </Text>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {guide.openGuide && (
            <Button size="sm" variant="primary" onClick={guide.openGuide}>
              <Button.Text>Read the guide</Button.Text>
            </Button>
          )}
          {/* Each guide has its own docs page, so there is no one page to
              send someone to when the lookup keys matched two. */}
          {guide.only && (
            <Button asChild size="sm" variant="secondary">
              <a
                href={docsUrl(guide.only)}
                target="_blank"
                rel="noopener noreferrer"
              >
                <Button.Text>Open the Docs</Button.Text>
                <Button.RightIcon>
                  <ExternalLink />
                </Button.RightIcon>
              </a>
            </Button>
          )}
        </div>
      </div>
    </StatusBanner>
  );
}

function describeSetupWork(only: MCPSetupGuide | undefined): string {
  if (only)
    return `${only.title} needs some setup before it will work in Gram.`;
  return "This server needs some setup before it will work in Gram.";
}
