import { HttpMethodColors } from "@/components/http-route";
import { Page } from "@/components/page-layout";
import { ToolBadge } from "@/components/tool-badge";
import { ToolsetBadge } from "@/components/tools-badge";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import {
  Carousel,
  CarouselContent,
  CarouselDots,
  CarouselItem,
  CarouselNext,
  CarouselPrevious,
} from "@/components/ui/carousel";
import { CopyButton } from "@/components/ui/copy-button";
import { Heading } from "@/components/ui/heading";
import { Link } from "@/components/ui/link";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { useTelemetry } from "@/contexts/Telemetry";
import { HumanizeDateTime } from "@/lib/dates";
import { useGroupedToolDefinitions } from "@/lib/toolNames";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { Toolset } from "@gram/client/models/components";
import { useListToolsets } from "@gram/client/react-query";
import { Grid, Icon, IconName, Stack } from "@speakeasy-api/moonshine";
import { ArrowRightIcon, CheckCircleIcon, LockIcon } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useMcpUrl } from "../mcp/MCPDetails";
import { useEmptyProjectRedirect } from "../openapi/OpenAPI";
import { ToolDefinition } from "../toolsets/types";

export default function Home() {
  const routes = useRoutes();
  const telemetry = useTelemetry();
  const { data: toolsets } = useListToolsets();
  const [selectedToolset, setSelectedToolset] = useState<Toolset | null>(null);

  useEffect(() => {
    if (toolsets?.toolsets.length) {
      setSelectedToolset(toolsets?.toolsets[0] ?? null);
    }
  }, [toolsets]);

  useEmptyProjectRedirect();

  const cards = [
    {
      icon: "message-circle",
      title: "Try it in the playground",
      description: "Try out the MCP server in the playground.",
      link: routes.playground.href(),
    },
    {
      icon: "blocks",
      title: "Curate your tools",
      description: "Create toolsets for different purposes.",
      link: routes.toolsets.toolset.href(selectedToolset?.slug ?? "_"),
    },
    {
      icon: "cog",
      title: "Configure your MCP server",
      description:
        "Choose your server settings. Like visibility, name, and more.",
      link: routes.mcp.details.href(selectedToolset?.slug ?? "_"),
    },
  ];

  const heroSection = useMemo(() => {
    return toolsets?.toolsets && toolsets.toolsets.length > 1 ? (
      <Carousel
        className="w-[calc(100%-60px)] self-center"
        onItemChange={(index) => {
          setSelectedToolset(toolsets?.toolsets[index] ?? null);
        }}
      >
        <CarouselContent>
          {toolsets?.toolsets.map((toolset) => (
            <CarouselItem key={toolset.slug}>
              <HeroCard toolset={toolset} />
            </CarouselItem>
          ))}
        </CarouselContent>
        <CarouselPrevious />
        <CarouselNext />
        <CarouselDots />
      </Carousel>
    ) : (
      <HeroCard toolset={toolsets?.toolsets[0]} />
    );
  }, [toolsets]);

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Stack className="mb-4">
          <Heading variant="h2" className="normal-case">
            Welcome to Gram
          </Heading>
          <Type>The easiest way to deploy MCP.</Type>
        </Stack>
        {heroSection}
        <Heading variant="h2" className="mt-5 mb-4">
          Getting started
        </Heading>
        <Grid columns={{ sm: 1, md: 2, lg: 3 }} gap={6}>
          {cards.map((card) => (
            <Grid.Item key={card.title}>
              <Card className="bg-sidebar h-[275px]">
                <Card.Content>
                  <Stack gap={4}>
                    <Icon
                      name={card.icon as IconName}
                      size="large"
                      className="text-muted-foreground"
                    />
                    <Link
                      to={card.link}
                      onClick={() =>
                        telemetry.capture("home_action", {
                          action: "card_clicked",
                          card: card.title,
                        })
                      }
                    >
                      <Stack
                        direction="horizontal"
                        gap={1}
                        align="center"
                        className="group"
                      >
                        <Heading variant="h4" className="font-normal">
                          {card.title}
                        </Heading>
                        <ArrowRightIcon className="w-4 h-4 text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity" />
                      </Stack>
                    </Link>
                    <Type className="text-[16px] leading-[28px]">
                      {card.description}
                    </Type>
                  </Stack>
                </Card.Content>
              </Card>
            </Grid.Item>
          ))}
        </Grid>
      </Page.Body>
    </Page>
  );
}

function HeroCard({
  toolset,
  className,
}: {
  toolset: Toolset | undefined;
  className?: string;
}) {
  const routes = useRoutes();
  const { url: mcpUrl, pageUrl } = useMcpUrl(toolset);

  const hero = (
    <div className="min-w-1/3 max-w-1/3 h-[300px] flex items-center justify-center border-1 rounded-lg bg-background relative overflow-clip">
      <HeroGraphic toolset={toolset} />
    </div>
  );

  return (
    <Card className={cn("w-full max-w-full relative bg-sidebar", className)}>
      <div className="absolute top-4 right-4">
        <routes.mcp.details.Link params={[toolset?.slug ?? "_"]}>
          <Button
            variant="ghost"
            size="icon"
            tooltip="MCP Settings"
            icon="cog"
          />
        </routes.mcp.details.Link>
      </div>
      <Card.Content>
        <Stack direction="horizontal" gap={6}>
          {hero}
          <Stack gap={6} className="py-4">
            <Stack>
              <Type muted>Toolset</Type>
              <Stack direction="horizontal" gap={2} align="center">
                <ToolsetBadge toolset={toolset} />
              </Stack>
            </Stack>
            <Stack direction="horizontal" gap={6}>
              <Stack>
                <Type muted>MCP URL</Type>
                <Stack direction="horizontal" align="center">
                  <Type className="font-medium" skeleton="phrase">
                    {mcpUrl}
                  </Type>
                  <CopyButton
                    text={mcpUrl ?? ""}
                    size="inline"
                    className="text-muted-foreground hover:text-foreground"
                  />
                </Stack>
              </Stack>
              <Stack>
                <Type muted>Install Page</Type>
                {toolset?.mcpIsPublic ? (
                  <Link to={pageUrl!} external>
                    <Type className="font-medium" skeleton="phrase">
                      {pageUrl}
                    </Type>
                  </Link>
                ) : (
                  <SimpleTooltip tooltip="Make this MCP public in order to access the shareable page.">
                    <Type className="font-medium" skeleton="phrase">
                      {pageUrl}
                    </Type>
                  </SimpleTooltip>
                )}
              </Stack>
            </Stack>
            <Stack gap={1}>
              <Type muted>Visibility</Type>
              <routes.mcp.details.Link params={[toolset?.slug ?? "_"]}>
                <Badge
                  variant="outline"
                  tooltip={"Change visibility in settings ->"}
                >
                  {toolset?.mcpIsPublic ? (
                    <Stack direction="horizontal" align="center" gap={2}>
                      <CheckCircleIcon className="w-4 h-4 text-green-400" />
                      <Type>Public</Type>
                    </Stack>
                  ) : (
                    <Stack direction="horizontal" align="center" gap={2}>
                      <LockIcon className="w-4 h-4 text-muted-foreground" />
                      <Type>Private</Type>
                    </Stack>
                  )}
                </Badge>
              </routes.mcp.details.Link>
            </Stack>
            <Stack direction="horizontal" gap={6}>
              <Stack>
                <Type muted>Created</Type>
                <Type>
                  <HumanizeDateTime date={toolset?.createdAt ?? new Date()} />
                </Type>
              </Stack>
              <Stack>
                <Type muted>Updated</Type>
                <Type>
                  <HumanizeDateTime date={toolset?.updatedAt ?? new Date()} />
                </Type>
              </Stack>
            </Stack>
          </Stack>
        </Stack>
      </Card.Content>
    </Card>
  );
}

function HeroGraphic({ toolset }: { toolset: Toolset | undefined }) {
  const groupedTools = useGroupedToolDefinitions(toolset);

  const groupedToolDefinitions =
    groupedTools.filter((group) => group.key !== "custom").length == 1
      ? groupedTools.flatMap((group) =>
          group.tools.map((tool) => ({
            ...tool,
            name: tool.displayName,
          }))
        )
      : groupedTools.flatMap((group) => group.tools);

  // Calculate how many badges we need to fill the space
  const badgesPerRow = 4;
  const estimatedRows = 13; // Adjust based on container height
  const totalBadgesNeeded = badgesPerRow * estimatedRows;

  // Fill remaining slots with empty badges, distributing them at beginning and end
  const allBadges: (ToolDefinition | null)[] = [...groupedToolDefinitions];
  const remainingSlots = totalBadgesNeeded - allBadges.length;

  if (remainingSlots > 0) {
    const frontPadding = Math.floor(remainingSlots / 2);
    const backPadding = remainingSlots - frontPadding;

    // Add empty badges at the beginning
    for (let i = 0; i < frontPadding; i++) {
      allBadges.unshift(null);
    }

    // Add empty badges at the end
    for (let i = 0; i < backPadding; i++) {
      allBadges.push(null);
    }
  }

  // Group badges into rows and add padding to each row
  const rows = [];
  for (let i = 0; i < allBadges.length; i += badgesPerRow) {
    const row = allBadges.slice(i, i + badgesPerRow);

    // Add 2 placeholders at beginning and end of each row
    const paddedRow = [null, null, ...row, null, null];
    rows.push(paddedRow);
  }

  return (
    <Stack
      className="absolute inset-0 w-[115%] h-[100%] rotate-[-11deg] -translate-x-1/8"
      direction="vertical"
      gap={2}
      justify={"center"}
      align="center"
    >
      {rows.map((row, rowIndex) => (
        <Stack
          key={rowIndex}
          direction="horizontal"
          gap={2}
          className={rowIndex % 2 === 1 ? "translate-x-8" : ""}
        >
          {row.map((tool, toolIndex) =>
            tool && "httpMethod" in tool ? (
              <ToolBadge
                tool={tool}
                key={`${rowIndex}-${toolIndex}`}
                variant={"outline"}
                className={cn(
                  "lowercase opacity-70 hover:opacity-100 trans",
                  HttpMethodColors[tool.httpMethod]?.border
                )}
              />
            ) : (
              <Badge
                key={`${rowIndex}-${toolIndex}`}
                variant={"secondary"}
                className={"opacity-40 w-30"}
                size="sm"
                isLoading={false}
              >
                {""}
              </Badge>
            )
          )}
        </Stack>
      ))}
    </Stack>
  );
}
