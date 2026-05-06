import { Button } from "@/components/ui/button";
import { CopyButton } from "@/components/ui/copy-button";
import { ExternalLink } from "lucide-react";

const COWORK_DOCS_URL =
  "https://support.claude.com/en/articles/13837433-manage-claude-cowork-plugins-for-your-organization";

type Props = {
  repoOwner: string;
  repoName: string;
  marketplaceUrl: string | undefined;
};

/**
 * Two-track install guidance for a published Gram plugin marketplace:
 *  - individual Claude Code users register the URL-based marketplace via
 *    the marketplace proxy
 *  - Claude Cowork org admins add the underlying private GitHub repo as a
 *    GitHub-source plugin in Organization settings; the proxy URL doesn't
 *    apply there because Cowork uses its own GitHub App for org-managed
 *    marketplaces.
 *
 * Visual style matches HooksSetupDialog so the two setup surfaces feel
 * consistent.
 */
export function InstallInstructions({
  repoOwner,
  repoName,
  marketplaceUrl,
}: Props) {
  const repoSlug = `${repoOwner}/${repoName}`;
  const installCommand = marketplaceUrl
    ? `/plugin marketplace add ${marketplaceUrl}`
    : null;

  return (
    <div className="max-w-3xl space-y-6">
      <div>
        <h3 className="mb-2 text-sm font-semibold">Install for yourself</h3>
        <p className="text-muted-foreground mb-4 text-sm">
          Register the marketplace in your Claude Code instance:
        </p>
        {installCommand ? (
          <div className="bg-muted/50 rounded-lg p-4 font-mono text-sm">
            <div className="flex items-center justify-between gap-2">
              <code className="break-all">{installCommand}</code>
              <CopyButton
                size="inline"
                text={installCommand}
                tooltip="Copy install command"
              />
            </div>
          </div>
        ) : (
          <p className="text-muted-foreground text-sm italic">
            Re-publish to mint a marketplace install URL.
          </p>
        )}
      </div>

      <div>
        <h3 className="mb-2 text-sm font-semibold">
          Distribute to your organization
        </h3>
        <p className="text-muted-foreground mb-4 text-sm">
          For Claude Cowork, register the underlying GitHub repository as a
          plugin source so the marketplace syncs to all org members.
        </p>

        <div className="space-y-4">
          <div>
            <h4 className="text-muted-foreground mb-2 text-xs font-medium">
              1. Open Organization settings
            </h4>
            <p className="text-muted-foreground text-sm">
              In Claude Desktop, navigate to{" "}
              <code className="bg-muted rounded px-1 py-0.5 text-xs">
                Organization settings → Plugins
              </code>
              , then click{" "}
              <code className="bg-muted rounded px-1 py-0.5 text-xs">
                Add plugin
              </code>
              .
            </p>
          </div>

          <div>
            <h4 className="text-muted-foreground mb-2 text-xs font-medium">
              2. Add the GitHub source
            </h4>
            <p className="text-muted-foreground mb-2 text-sm">
              Select{" "}
              <code className="bg-muted rounded px-1 py-0.5 text-xs">
                GitHub
              </code>{" "}
              as the source and enter your repo:
            </p>
            <div className="bg-muted/50 rounded-lg p-4 font-mono text-sm">
              <div className="flex items-center justify-between gap-2">
                <code className="break-all">{repoSlug}</code>
                <CopyButton
                  size="inline"
                  text={repoSlug}
                  tooltip="Copy repository slug"
                />
              </div>
            </div>
          </div>

          <div>
            <h4 className="text-muted-foreground mb-2 text-xs font-medium">
              3. Authorize Claude's GitHub App
            </h4>
            <p className="text-muted-foreground text-sm">
              The Claude GitHub App must be installed on this repository so
              Cowork can sync from it. If the repo doesn't appear in the picker,
              install the app and retry.
            </p>
          </div>

          <Button variant="outline" size="sm" asChild>
            <a
              href={COWORK_DOCS_URL}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-2"
            >
              <ExternalLink className="size-4" />
              Cowork setup guide
            </a>
          </Button>
        </div>
      </div>
    </div>
  );
}
