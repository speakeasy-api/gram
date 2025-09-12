import { Badge } from "@/components/ui/badge";
import { Button } from "@speakeasy-api/moonshine";
import { Card } from "@/components/ui/card";
import { CopyButton } from "@/components/ui/copy-button";
import { Action, MoreActions } from "@/components/ui/more-actions";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Switch } from "@/components/ui/switch";
import { ServerEnableDialog } from "@/components/server-enable-dialog";
import { UpdatedAt } from "@/components/updated-at";
import { useTelemetry } from "@/contexts/Telemetry";
import { cn } from "@/lib/utils";
import { useMcpUrl } from "@/pages/mcp/MCPDetails";
import { useRoutes } from "@/routes";
import { ToolsetEntry } from "@gram/client/models/components";
import {
  invalidateAllGetPeriodUsage,
  invalidateAllListToolsets,
  useUpdateToolsetMutation,
} from "@gram/client/react-query";
import { Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import {
  CheckCircleIcon,
  ExternalLinkIcon,
  LockIcon,
  MessageCircleIcon,
  XCircleIcon,
} from "lucide-react";
import { useState } from "react";

interface ServerCardProps {
  toolset: ToolsetEntry | undefined;
  className?: string;
  onCardClick?: () => void;
  additionalActions?: Action[];
}

export function ServerCard({
  toolset,
  className,
  onCardClick,
  additionalActions = [],
}: ServerCardProps) {
  const routes = useRoutes();
  const telemetry = useTelemetry();
  const { url: mcpUrl, pageUrl } = useMcpUrl(toolset);
  const updateToolsetMutation = useUpdateToolsetMutation();
  const queryClient = useQueryClient();
  const [isServerEnableDialogOpen, setIsServerEnableDialogOpen] = useState(false);

  if (!toolset) return null;

  const handleCardClick = () => {
    if (onCardClick) {
      onCardClick();
    } else {
      routes.toolsets.toolset.goTo(toolset.slug);
    }
  };

  const handlePrivacyToggle = async (isPublic: boolean) => {
    updateToolsetMutation.mutate(
      {
        request: {
          slug: toolset.slug,
          updateToolsetRequestBody: {
            mcpIsPublic: isPublic,
          },
        },
      },
      {
        onSuccess: () => {
          invalidateAllListToolsets(queryClient);

          telemetry.capture("server_card_action", {
            action: "privacy_toggle",
            server: toolset.slug,
            newState: isPublic ? "public" : "private",
          });
        },
      }
    );
  };

  const handleServerEnabledToggle = async () => {
    updateToolsetMutation.mutate(
      {
        request: {
          slug: toolset.slug,
          updateToolsetRequestBody: {
            mcpEnabled: !toolset.mcpEnabled,
          },
        },
      },
      {
        onSuccess: () => {
          invalidateAllListToolsets(queryClient);
          invalidateAllGetPeriodUsage(queryClient);

          telemetry.capture("server_card_action", {
            action: "server_enabled_toggle",
            server: toolset.slug,
            newState: !toolset.mcpEnabled ? "enabled" : "disabled",
          });
        },
      }
    );
  };

  const handleSwitchClick = () => {
    setIsServerEnableDialogOpen(true);
  };

  const defaultActions: Action[] = [
    {
      label: "Manage Tools",
      onClick: () => routes.toolsets.toolset.goTo(toolset.slug),
      icon: "blocks",
    },
    {
      label: "MCP Settings",
      onClick: () => routes.mcp.details.goTo(toolset.slug),
      icon: "cog",
    },
  ];

  const allActions = [...additionalActions, ...defaultActions];

  let serverStateBadge = toolset.mcpIsPublic ? (
    <Badge
      variant="outline"
      size="sm"
      className="cursor-pointer hover:bg-green-50 transition-colors flex items-center gap-1 uppercase font-mono"
      onClick={(e) => e.stopPropagation()}
    >
      <CheckCircleIcon className="w-3 h-3 text-green-500" />
      Public
    </Badge>
  ) : (
    <Badge
      variant="outline"
      size="sm"
      className="cursor-pointer hover:bg-muted/50 transition-colors flex items-center gap-1 uppercase font-mono"
      onClick={(e) => e.stopPropagation()}
    >
      <LockIcon className="w-3 h-3 text-muted-foreground" />
      Private
    </Badge>
  );

  if (!toolset.mcpEnabled) {
    serverStateBadge = (
      <Badge
        variant="outline"
        size="sm"
        className="cursor-pointer hover:bg-muted/50 transition-colors flex items-center gap-1 uppercase font-mono text-muted-foreground"
        onClick={(e) => e.stopPropagation()}
      >
        <XCircleIcon className="w-3 h-3 text-muted-foreground" />
        Disabled
      </Badge>
    );
  }

  return (
    <Card
      className={cn(className, "group transition-colors hover:bg-muted/50")}
    >
      <Card.Header
        className="cursor-pointer items-start"
        onClick={handleCardClick}
      >
        <div className="flex flex-col gap-1">
          <Card.Title className="text-base">
            <div className="flex items-center gap-1 group">
              {toolset.name}
              <CopyButton
                text={toolset.slug}
                size="icon-sm"
                tooltip="Copy slug"
                className="text-muted-foreground/80 hover:text-foreground opacity-0 group-hover:opacity-100"
              />
            </div>
          </Card.Title>
          <UpdatedAt date={new Date(toolset.updatedAt)} italic={false} />
        </div>
        <MoreActions actions={allActions} />
      </Card.Header>
      <Card.Content className="cursor-pointer" onClick={handleCardClick}>
        <Card.Description>
          A toolset created from your OpenAPI document
        </Card.Description>
      </Card.Content>
      <Card.Footer>
        <Stack direction="horizontal" gap={2} align="center">
          {/* Status Badge with Privacy Controls */}
          <Popover>
            <PopoverTrigger asChild>{serverStateBadge}</PopoverTrigger>
            <PopoverContent
              className="w-80"
              align="start"
              onClick={(e) => e.stopPropagation()}
            >
              <div className="space-y-4">
                {/* Server Enabled Toggle Section */}
                <div className="space-y-3">
                  <div className="flex items-center justify-between">
                    <div>
                      <h4 className="text-base font-light">Server Enabled</h4>
                      <p className="text-xs text-muted-foreground">
                        {toolset.mcpEnabled
                          ? "Server is active, can receive requests"
                          : "Server is disabled, cannot receive requests"}
                      </p>
                    </div>
                    <Switch
                      checked={toolset.mcpEnabled ?? false}
                      onCheckedChange={handleSwitchClick}
                      disabled={updateToolsetMutation.isPending}
                      aria-label={`Toggle server enabled. Currently ${
                        toolset.mcpEnabled ? "enabled" : "disabled"
                      }`}
                    />
                  </div>
                </div>

                {/* Only show other sections when server is enabled */}
                {toolset.mcpEnabled && (
                  <>
                    {/* Privacy Toggle Section */}
                    <div className="space-y-3">
                      <div className="flex items-center justify-between">
                        <div>
                          <h4 className="text-base font-light">
                            Server Privacy
                          </h4>
                          <p className="text-xs text-muted-foreground">
                            {toolset.mcpIsPublic
                              ? "Publicly accessible and installable"
                              : "Private, only accessible to you"}
                          </p>
                        </div>
                        <Switch
                          checked={toolset.mcpIsPublic ?? false}
                          onCheckedChange={handlePrivacyToggle}
                          disabled={updateToolsetMutation.isPending}
                          aria-label={`Toggle server privacy. Currently ${
                            toolset.mcpIsPublic ? "public" : "private"
                          }`}
                        />
                      </div>
                    </div>

                    <div className="flex flex-col gap-3 pt-3">
                      {/* MCP URL Section */}
                      {mcpUrl && (
                        <div className="space-y-2">
                          <label className="text-xs text-muted-foreground">
                            MCP URL
                          </label>
                          <div className="flex items-center gap-3">
                            <code className="flex-1 text-xs bg-muted/50 px-2 py-1 rounded border text-muted-foreground font-mono overflow-x-auto whitespace-nowrap min-w-0">
                              {mcpUrl}
                            </code>
                            <div className="flex-shrink-0">
                              <CopyButton text={mcpUrl} size="icon-sm" />
                            </div>
                          </div>
                        </div>
                      )}

                      {/* Install URL Section */}
                      <div className="space-y-2">
                        <label className="text-xs text-muted-foreground">
                          Install Page
                        </label>
                        {toolset.mcpIsPublic && pageUrl ? (
                          <div className="flex items-center gap-3">
                            <code className="flex-1 text-xs bg-muted/50 px-2 py-1 rounded border text-muted-foreground font-mono overflow-x-auto whitespace-nowrap min-w-0">
                              {pageUrl}
                            </code>
                            <div className="flex-shrink-0">
                              <CopyButton text={pageUrl} size="icon-sm" />
                            </div>
                          </div>
                        ) : (
                          <>
                            {pageUrl ? (
                              <div className="flex items-center gap-3">
                                <code className="flex-1 text-xs bg-muted/50 px-2 py-1 rounded border text-muted-foreground font-mono overflow-x-auto whitespace-nowrap min-w-0">
                                  {pageUrl}
                                </code>
                                <div className="flex-shrink-0">
                                  <CopyButton text={pageUrl} size="icon-sm" />
                                </div>
                              </div>
                            ) : (
                              <div className="flex items-center gap-3">
                                <code className="flex-1 text-xs bg-muted/30 px-2 py-1 rounded border border-dashed text-muted-foreground font-mono">
                                  Will be generated when public
                                </code>
                              </div>
                            )}
                            <p className="text-xs text-muted-foreground">
                              Install page will be available once server is made
                              public
                            </p>
                          </>
                        )}
                      </div>
                    </div>

                    <div className="pt-2">
                      <Button
                        variant="secondary"
                        size="sm"
                        onClick={(e) => {
                          e.preventDefault();
                          routes.mcp.details.goTo(toolset.slug);
                        }}
                        className="w-full"
                      >
                        ADVANCED SETTINGS
                      </Button>
                    </div>
                  </>
                )}
              </div>
            </PopoverContent>
          </Popover>

          {/* Quick Test Badge */}
          <div
            onClick={(e) => {
              e.preventDefault();
              e.stopPropagation();
              telemetry.capture("server_card_action", {
                action: "test",
                server: toolset.slug,
              });
              routes.playground.goTo(toolset.slug);
            }}
          >
            <Badge
              variant="outline"
              size="sm"
              className="cursor-pointer hover:bg-muted/50 transition-colors flex items-center gap-1 uppercase font-mono"
            >
              <MessageCircleIcon className="w-3 h-3" />
              Test
            </Badge>
          </div>

          {/* Install Badge for Public Servers */}
          {toolset.mcpIsPublic && pageUrl && (
            <div
              onClick={(e) => {
                e.preventDefault();
                e.stopPropagation();
                window.open(pageUrl, "_blank");
                telemetry.capture("server_card_action", {
                  action: "install",
                  server: toolset.slug,
                });
              }}
            >
              <Badge
                variant="outline"
                size="sm"
                className="cursor-pointer hover:bg-muted/50 transition-colors flex items-center gap-1 uppercase font-mono"
              >
                <ExternalLinkIcon className="w-3 h-3" />
                Install
              </Badge>
            </div>
          )}
        </Stack>
      </Card.Footer>
      
      <ServerEnableDialog
        isOpen={isServerEnableDialogOpen}
        onClose={() => setIsServerEnableDialogOpen(false)}
        onConfirm={handleServerEnabledToggle}
        isLoading={updateToolsetMutation.isPending}
        currentlyEnabled={toolset.mcpEnabled ?? false}
      />
    </Card>
  );
}
