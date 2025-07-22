import { CopyableSlug } from "@/components/name-and-slug";
import {
  ToolsetPromptsBadge,
  ToolsetToolsBadge,
} from "@/components/tools-badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { MoreActions } from "@/components/ui/more-actions";
import { Type } from "@/components/ui/type";
import { HumanizeDateTime } from "@/lib/dates";
import { useRoutes } from "@/routes";
import { Toolset } from "@gram/client/models/components";
import { Stack } from "@speakeasy-api/moonshine";
import { useDeleteToolset } from "./Toolset";

export function ToolsetCard({
  toolset,
  className,
}: {
  toolset: Toolset;
  className?: string;
}) {
  const routes = useRoutes();
  const deleteToolset = useDeleteToolset();

  return (
    <Card className={className}>
      <Card.Header>
        <Card.Title>
          <CopyableSlug slug={toolset.slug}>
            <routes.toolsets.toolset.Link params={[toolset.slug]}>
              {toolset.name}
            </routes.toolsets.toolset.Link>
          </CopyableSlug>
        </Card.Title>
        <MoreActions
          actions={[
            {
              label: "Add Tools",
              onClick: () => routes.toolsets.toolset.update.goTo(toolset.slug),
              icon: "pencil",
            },
            {
              label: "Delete",
              onClick: () => deleteToolset(toolset.slug),
              destructive: true,
              icon: "trash",
            },
          ]}
        />
      </Card.Header>
      <Card.Content>
        <Card.Description>{toolset.description}</Card.Description>
      </Card.Content>
      <Card.Footer>
        <Stack direction="horizontal" gap={1} align="center">
          <ToolsetToolsBadge toolset={toolset} />
          <ToolsetPromptsBadge toolset={toolset} />
        </Stack>
        <Type muted small italic>
          {"Updated "}
          <HumanizeDateTime date={new Date(toolset.updatedAt)} />
        </Type>
      </Card.Footer>
    </Card>
  );
}

export function ToolsetPlaygroundLink({
  toolset,
}: {
  toolset: Toolset | undefined;
}) {
  const routes = useRoutes();
  return (
    <routes.playground.Link
      queryParams={{ ...(toolset ? { toolset: toolset.slug } : {}) }}
    >
      <Button
        variant="outline"
        className="group"
        tooltip="Open in chat playground"
        caps
      >
        Playground
        <routes.playground.Icon className="text-muted-foreground group-hover:text-foreground trans" />
      </Button>
    </routes.playground.Link>
  );
}
