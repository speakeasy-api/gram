import { CopyableSlug } from "@/components/name-and-slug";
import {
  ToolCollectionBadge,
  ToolsetPromptsBadge,
} from "@/components/tool-collection-badge";
import { Card } from "@/components/ui/card";
import { MoreActions } from "@/components/ui/more-actions";
import { UpdatedAt } from "@/components/updated-at";
import { useRoutes } from "@/routes";
import { ToolsetEntry } from "@gram/client/models/components";
import { Button, cn, Stack } from "@speakeasy-api/moonshine";
import { useCloneToolset, useDeleteToolset } from "./Toolset";

type ToolsetForCard = Pick<
  ToolsetEntry,
  | "id"
  | "name"
  | "slug"
  | "description"
  | "updatedAt"
  | "tools"
  | "promptTemplates"
>;

const BoundToolsBadge = ({
  toolset,
  className,
}: {
  toolset: ToolsetForCard;
  size?: "sm" | "md";
  variant?: "outline" | "default";
  className?: string;
}) => {
  const names: string[] = toolset?.tools.map((tool) => tool.name) ?? [];

  return (
    <ToolCollectionBadge
      toolNames={names}
      className={className}
      warnOnTooManyTools
    />
  );
};

export function ToolsetCard({
  toolset,
  className,
}: {
  toolset: ToolsetForCard;
  className?: string;
}) {
  const routes = useRoutes();
  const deleteToolset = useDeleteToolset();
  const cloneToolset = useCloneToolset();

  return (
    <routes.toolsets.toolset.Link
      params={[toolset.slug]}
      className="hover:no-underline"
    >
      <Card className={cn(className)}>
        <Card.Header>
          <Card.Title>
            <CopyableSlug slug={toolset.slug}>{toolset.name}</CopyableSlug>
          </Card.Title>
          <MoreActions
            actions={[
              {
                label: "Add Tools",
                onClick: () => routes.toolsets.toolset.goTo(toolset.slug),
                icon: "pencil",
              },
              {
                label: "Playground",
                onClick: () => routes.playground.goTo(toolset.slug),
                icon: "message-circle",
              },
              {
                label: "Clone",
                onClick: () => cloneToolset(toolset.slug),
                icon: "copy",
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
            <BoundToolsBadge toolset={toolset} />
            <ToolsetPromptsBadge toolset={toolset} />
          </Stack>
          <UpdatedAt date={new Date(toolset.updatedAt)} />
        </Card.Footer>
      </Card>
    </routes.toolsets.toolset.Link>
  );
}

export function ToolsetPlaygroundLink({
  toolset,
}: {
  toolset: Pick<ToolsetEntry, "slug">;
}) {
  const routes = useRoutes();
  return (
    <routes.playground.Link
      queryParams={{ ...(toolset ? { toolset: toolset.slug } : {}) }}
    >
      <Button variant="secondary" className="group">
        PLAYGROUND
        <routes.playground.Icon className="text-muted-foreground group-hover:text-foreground trans" />
      </Button>
    </routes.playground.Link>
  );
}
