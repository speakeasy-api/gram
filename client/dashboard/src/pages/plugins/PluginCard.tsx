import { DotCard } from "@/components/ui/dot-card";
import { Type } from "@/components/ui/type";
import { HumanizeDateTime } from "@/lib/dates";
import { useRoutes } from "@/routes";
import { useSdkClient } from "@/contexts/Sdk";
import type { Plugin } from "@gram/client/models/components/plugin.js";
import type { PublishStatusResult } from "@gram/client/models/components/publishstatusresult.js";
import {
  Badge,
  Button,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
  Icon,
} from "@speakeasy-api/moonshine";
import { ArrowRight, Puzzle, Server } from "lucide-react";
import { useState } from "react";
import { Link, useNavigate } from "react-router";
import { toast } from "sonner";
import {
  DEFAULT_PLUGIN_DESCRIPTION,
  isDefaultPluginSlug,
} from "./default-plugin";
import { downloadPluginPackage } from "./downloadPluginPackage";
import { InstallInstructionsDialog } from "./InstallInstructionsDialog";

export function PluginCard({
  plugin,
  publishStatus,
}: {
  plugin: Plugin;
  publishStatus: PublishStatusResult | undefined;
}): JSX.Element {
  const routes = useRoutes();
  const navigate = useNavigate();
  const client = useSdkClient();
  const detailHref = routes.plugins.detail.href(plugin.id);
  const serverCount = plugin.serverCount ?? 0;
  const isDefault = isDefaultPluginSlug(plugin.slug);
  const description =
    plugin.description ?? (isDefault ? DEFAULT_PLUGIN_DESCRIPTION : undefined);
  const [isInstallOpen, setIsInstallOpen] = useState(false);
  const [isDownloadMenuOpen, setIsDownloadMenuOpen] = useState(false);
  const installTarget =
    publishStatus?.connected &&
    publishStatus.repoOwner &&
    publishStatus.repoName
      ? {
          repoOwner: publishStatus.repoOwner,
          repoName: publishStatus.repoName,
          marketplaceUrl: publishStatus.marketplaceUrl,
        }
      : undefined;

  const handleDownload = async (platform: "claude" | "cursor" | "codex") => {
    setIsDownloadMenuOpen(false);
    try {
      await downloadPluginPackage(client, plugin.id, platform);
    } catch (_err) {
      toast.error("Failed to download plugin package");
    }
  };

  return (
    <div>
      <DotCard
        className="cursor-pointer"
        onClick={() => {
          void navigate(detailHref);
        }}
        icon={<Puzzle className="text-muted-foreground h-10 w-10 opacity-60" />}
      >
        <div className="mb-2 flex items-start justify-between gap-2">
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-1.5">
              <Type
                variant="subheading"
                as="div"
                className="text-md group-hover:text-primary truncate transition-colors"
                title={plugin.name}
              >
                {plugin.name}
              </Type>
              {isDefault && (
                <Badge variant="information">
                  <Badge.Text>Default</Badge.Text>
                </Badge>
              )}
            </div>
            <Type
              small
              muted
              className="truncate font-mono"
              title={plugin.slug}
            >
              {plugin.slug}
            </Type>
          </div>
          <Badge variant="neutral" className="shrink-0">
            <Badge.LeftIcon>
              <Server className="h-3 w-3" />
            </Badge.LeftIcon>
            <Badge.Text>
              {serverCount} {serverCount === 1 ? "server" : "servers"}
            </Badge.Text>
          </Badge>
        </div>

        {description && (
          <Type small muted className="mb-1 line-clamp-3">
            {description}
          </Type>
        )}
        <Type small className="text-muted-foreground/60 mt-2 mb-3">
          Updated <HumanizeDateTime date={plugin.updatedAt} />
        </Type>

        <div className="mt-auto flex items-center justify-end gap-2 pt-2">
          <div className="flex items-center gap-2">
            <div onClick={(e) => e.stopPropagation()}>
              <DropdownMenu
                open={isDownloadMenuOpen}
                onOpenChange={setIsDownloadMenuOpen}
              >
                <DropdownMenuTrigger asChild>
                  <Button variant="primary" size="sm" disabled={!installTarget}>
                    <Button.Text>Install</Button.Text>
                    <span className="bg-primary-foreground/25 mx-1 h-4 w-px self-center" />
                    <Button.RightIcon>
                      <Icon name="chevron-down" className="h-4 w-4" />
                    </Button.RightIcon>
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuItem
                    onClick={() => {
                      // Defer until after the dropdown has fully closed to
                      // avoid a Radix focus-trap/body-lock conflict between
                      // the closing menu and the opening sheet (same pattern
                      // as MCPDetails.tsx).
                      setTimeout(() => setIsInstallOpen(true), 0);
                    }}
                  >
                    Install instructions
                  </DropdownMenuItem>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem
                    onClick={() => {
                      void handleDownload("claude");
                    }}
                  >
                    Download as zip — Claude
                  </DropdownMenuItem>
                  <DropdownMenuItem
                    onClick={() => {
                      void handleDownload("cursor");
                    }}
                  >
                    Download as zip — Cursor
                  </DropdownMenuItem>
                  <DropdownMenuItem
                    onClick={() => {
                      void handleDownload("codex");
                    }}
                  >
                    Download as zip — Codex
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
            <Link to={detailHref} onClick={(e) => e.stopPropagation()}>
              <Button variant="secondary" size="sm">
                <Button.Text>View</Button.Text>
                <Button.RightIcon>
                  <ArrowRight className="h-4 w-4" />
                </Button.RightIcon>
              </Button>
            </Link>
          </div>
        </div>
      </DotCard>
      {installTarget && (
        <div onClick={(e) => e.stopPropagation()}>
          <InstallInstructionsDialog
            open={isInstallOpen}
            onOpenChange={setIsInstallOpen}
            repoOwner={installTarget.repoOwner}
            repoName={installTarget.repoName}
            marketplaceUrl={installTarget.marketplaceUrl}
            candidatePlugins={[
              {
                name: plugin.name,
                slug: plugin.slug,
                description: plugin.description,
              },
            ]}
          />
        </div>
      )}
    </div>
  );
}
