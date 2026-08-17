import { CardContextMenu } from "@/components/card-context-menu";
import { Card } from "@/components/ui/Card";
import type { Action } from "@/components/ui/MoreActions";
import { Text } from "@/components/ui/Text";
import { HumanizeDateTime } from "@/lib/dates";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { useSdkClient } from "@/contexts/Sdk";
import type { Plugin } from "@gram/client/models/components/plugin.js";
import type { PublishStatusResult } from "@gram/client/models/components/publishstatusresult.js";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/Dropdown";
import { Icon } from "@/components/ui/Icon";
import { ArrowRight, Puzzle, Server } from "lucide-react";
import { Fragment, useState } from "react";
import { Link, useNavigate } from "react-router";
import { DEFAULT_PLUGIN_DESCRIPTION } from "./default-plugin";
import { usePluginPackageDownload } from "./downloadPluginPackage";
import { InstallInstructionsDialog } from "./InstallInstructionsDialog";
import { PluginInstallButton } from "./PluginInstallButton";

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
  const skillCount = plugin.skillCount ?? 0;
  const isDefault = plugin.isDefault ?? false;
  const description =
    plugin.description ?? (isDefault ? DEFAULT_PLUGIN_DESCRIPTION : undefined);
  const [isInstallOpen, setIsInstallOpen] = useState(false);
  const [isDownloadMenuOpen, setIsDownloadMenuOpen] = useState(false);
  const { isDownloading, download } = usePluginPackageDownload(
    client,
    plugin.id,
    setIsDownloadMenuOpen,
  );
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

  // Single source of truth for the Install split-button dropdown. The
  // right-click context menu reuses these entries (plus View) so every card
  // action is reachable from either menu without being defined twice.
  const installActions: Action[] = [
    {
      label: "GitHub installation (preferred)",
      disabled: !installTarget,
      description: installTarget ? undefined : "Requires marketplace setup",
      onClick: () => {
        // Defer until after the menu has fully closed to avoid a Radix
        // focus-trap/body-lock conflict between the closing menu and the
        // opening sheet (same pattern as MCPDetails.tsx).
        setTimeout(() => setIsInstallOpen(true), 0);
      },
    },
    {
      label: "Download as zip — Claude",
      separatorBefore: true,
      disabled: isDownloading,
      onClick: () => {
        void download("claude");
      },
    },
    {
      label: "Download as zip — Cursor",
      disabled: isDownloading,
      onClick: () => {
        void download("cursor");
      },
    },
    {
      label: "Download as zip — Codex",
      disabled: isDownloading,
      onClick: () => {
        void download("codex");
      },
    },
    ...(plugin.agentPluginsV1Compatible
      ? [
          {
            label: "Download Agent Plugins ZIP",
            disabled: isDownloading,
            onClick: () => {
              void download("agent-plugin");
            },
          },
        ]
      : []),
  ];

  const actions: Action[] = [
    ...installActions,
    {
      label: "View",
      onClick: () => {
        void navigate(detailHref);
      },
    },
  ];

  return (
    <CardContextMenu actions={actions}>
      <div>
        <Card.Entity
          className="cursor-pointer max-sm:h-auto max-sm:[&>div:first-child]:w-24"
          onClick={() => {
            void navigate(detailHref);
          }}
          icon={
            <Puzzle className="text-muted-foreground h-10 w-10 opacity-60" />
          }
        >
          <div className="mb-2 flex items-start justify-between gap-2">
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-1.5">
                <Text
                  variant="subheading"
                  as="div"
                  className="text-md group-hover:text-primary truncate transition-colors"
                  title={plugin.name}
                >
                  {plugin.name}
                </Text>
                {isDefault && (
                  <Badge variant="information">
                    <Badge.Text>Default</Badge.Text>
                  </Badge>
                )}
                {publishStatus?.upToDate === false && (
                  <Badge variant="warning">
                    <Badge.Text>Needs syncing</Badge.Text>
                  </Badge>
                )}
              </div>
              <Text
                small
                muted
                className="truncate font-mono"
                title={plugin.slug}
              >
                {plugin.slug}
              </Text>
            </div>
            <div className="flex shrink-0 items-center gap-2">
              <Badge variant="neutral">
                <Badge.LeftIcon>
                  <Server className="h-3 w-3" />
                </Badge.LeftIcon>
                <Badge.Text>
                  {serverCount} {serverCount === 1 ? "server" : "servers"}
                </Badge.Text>
              </Badge>
              {skillCount > 0 && (
                <Badge variant="neutral">
                  <Badge.LeftIcon>
                    <Icon name="terminal" className="h-3 w-3" />
                  </Badge.LeftIcon>
                  <Badge.Text>
                    {skillCount} {skillCount === 1 ? "skill" : "skills"}
                  </Badge.Text>
                </Badge>
              )}
            </div>
          </div>

          {description && (
            <Text small muted className="mb-1 line-clamp-3">
              {description}
            </Text>
          )}
          <Text small className="text-muted-foreground/60 mt-2 mb-3">
            {publishStatus?.lastPublishedAt ? (
              <>
                Published{" "}
                <HumanizeDateTime date={publishStatus.lastPublishedAt} />
              </>
            ) : (
              <>
                Updated <HumanizeDateTime date={plugin.updatedAt} />
              </>
            )}
          </Text>

          <div className="mt-auto flex items-center justify-between gap-2 pt-2">
            <div onClick={(e) => e.stopPropagation()}>
              <AgentPluginsStatus
                compatible={plugin.agentPluginsV1Compatible}
              />
            </div>
            <div className="flex items-center gap-2">
              <div onClick={(e) => e.stopPropagation()}>
                <DropdownMenu
                  open={isDownloadMenuOpen}
                  onOpenChange={setIsDownloadMenuOpen}
                >
                  <DropdownMenuTrigger asChild>
                    <PluginInstallButton size="sm" loading={isDownloading} />
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end">
                    {installActions.map((action) => (
                      <Fragment key={action.label}>
                        {action.separatorBefore && <DropdownMenuSeparator />}
                        <DropdownMenuItem
                          disabled={action.disabled}
                          onClick={action.onClick}
                        >
                          {action.description ? (
                            <div className="flex flex-col">
                              <span>{action.label}</span>
                              <span className="text-muted-foreground text-xs">
                                {action.description}
                              </span>
                            </div>
                          ) : (
                            action.label
                          )}
                        </DropdownMenuItem>
                      </Fragment>
                    ))}
                  </DropdownMenuContent>
                </DropdownMenu>
              </div>
              <Link to={detailHref} onClick={(e) => e.stopPropagation()}>
                <Button
                  variant="secondary"
                  size="sm"
                  aria-label="View plugin"
                  className="max-sm:w-8 max-sm:px-0"
                >
                  <Button.Text className="max-sm:sr-only">View</Button.Text>
                  <Button.RightIcon>
                    <ArrowRight className="h-4 w-4" />
                  </Button.RightIcon>
                </Button>
              </Link>
            </div>
          </div>
        </Card.Entity>
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
    </CardContextMenu>
  );
}

function AgentPluginsStatus({
  compatible,
}: {
  compatible: boolean;
}): JSX.Element {
  const label = compatible
    ? "Agent Plugin standard compatible"
    : "Not Agent Plugin standard compatible";
  const tooltip = compatible
    ? "Agent Plugin standard compatible. Additional harnesses can use this plugin: https://agent-plugins.org/compatible-clients"
    : "Not Agent Plugin standard compatible. Plugin works normally in our supported harnesses.";

  return (
    <SimpleTooltip tooltip={tooltip}>
      <span
        role="img"
        tabIndex={0}
        aria-label={label}
        onKeyDown={(event) => event.stopPropagation()}
        className={cn(
          "inline-flex h-8 w-8 items-center justify-center border",
          compatible
            ? "border-success-softest bg-success-softest text-default-success"
            : "border-border bg-muted text-muted-foreground",
        )}
      >
        {/* Official mark from https://agent-plugins.org/. */}
        <svg
          viewBox="0 0 18 17"
          aria-hidden="true"
          className="h-4 w-4"
          fill="currentColor"
        >
          <path d="M2.89502 12.7607V13.8984L3.56006 14.5381H14.4399L15.104 13.8994V12.7607H16.7417V14.5967L15.3364 15.9473L15.0981 16.1758H2.90088L2.66357 15.9473L1.50928 14.8389L1.2583 14.5967V12.7607H2.89502ZM15.0981 0L15.3364 0.228516L16.4897 1.33691L16.7417 1.57812V3.41406H15.104V2.27539L14.439 1.63672H3.56006L2.89502 2.27539V3.41406H1.2583V1.57812L2.66357 0.228516L2.90088 0H15.0981Z" />
          <path d="M16.3628 8.12036C16.3628 7.24277 15.6515 6.53157 14.7739 6.53149C13.8963 6.53149 13.1851 7.24272 13.1851 8.12036C13.1851 8.99793 13.8963 9.70923 14.7739 9.70923V11.3469L14.6079 11.342C12.9581 11.2585 11.635 9.93612 11.5513 8.28638L11.5474 8.12036C11.5474 6.33846 12.992 4.8938 14.7739 4.8938L14.9399 4.89771C16.6446 4.98418 18.0005 6.39419 18.0005 8.12036L17.9956 8.28638C17.9091 9.991 16.5 11.3468 14.7739 11.3469V9.70923C15.6514 9.70915 16.3627 8.99788 16.3628 8.12036Z" />
          <path d="M6.45283 8.12021C6.45283 9.90211 5.00831 11.3466 3.22641 11.3466C1.44451 11.3466 0 9.90211 0 8.12021C0 6.33831 1.44451 4.8938 3.22641 4.8938C5.00831 4.8938 6.45283 6.33831 6.45283 8.12021Z" />
          <path d="M5.14258 8.12036C5.14258 7.06225 4.28466 6.20443 3.22656 6.20435C2.1684 6.20435 1.31055 7.0622 1.31055 8.12036C1.31063 9.17846 2.16845 10.0364 3.22656 10.0364V11.3469L3.06055 11.342C1.41077 11.2585 0.087592 9.93612 0.00390625 8.28638L0 8.12036C0 6.33846 1.44466 4.8938 3.22656 4.8938L3.39258 4.89771C5.09726 4.98418 6.45312 6.39419 6.45312 8.12036L6.44824 8.28638C6.36175 9.991 4.95266 11.3468 3.22656 11.3469V10.0364C4.28461 10.0363 5.1425 9.17841 5.14258 8.12036Z" />
          <path d="M10.384 7.31519V8.95288H4.84595V7.31519H10.384Z" />
        </svg>
      </span>
    </SimpleTooltip>
  );
}
