import { CopyableSlug } from "@/components/name-and-slug";
import {
  ToolsetPromptsBadge,
  ToolsetToolsBadge,
} from "@/components/tools-badge";
import { Button } from "@speakeasy-api/moonshine";
import { Card } from "@/components/ui/card";
import { MoreActions } from "@/components/ui/more-actions";
import { useRoutes } from "@/routes";
import { ToolsetEntry } from "@gram/client/models/components";
import { Stack } from "@speakeasy-api/moonshine";
import { useDeleteToolset } from "./Toolset";
import { UpdatedAt } from "@/components/updated-at";

type ToolsetForCard = Pick<ToolsetEntry, 'id' | 'name' | 'slug' | 'description' | 'updatedAt' | 'httpTools' | 'promptTemplates'>;

export function ToolsetCard({
  toolset,
  className,
}: {
  toolset: ToolsetForCard;
  className?: string;
}) {
  const routes = useRoutes();
  const deleteToolset = useDeleteToolset();

  return (
    <routes.toolsets.toolset.Link params={[toolset.slug]} className="hover:no-underline">
      <Card className={className}>
        <Card.Header>
          <Card.Title>
            <CopyableSlug slug={toolset.slug}>
                {toolset.name}
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
                label: "Playground",
                onClick: () => routes.playground.goTo(toolset.slug),
                icon: "message-circle",
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
          <UpdatedAt date={new Date(toolset.updatedAt)} />
        </Card.Footer>
      </Card>
    </routes.toolsets.toolset.Link>
  );
}

export function ToolsetPlaygroundLink({
  toolset,
}: {
  toolset: Pick<ToolsetEntry, 'slug'>;
}) {
  const routes = useRoutes();
  return (
    <routes.playground.Link
      queryParams={{ ...(toolset ? { toolset: toolset.slug } : {}) }}
    >
      <Button
        variant="secondary"
        className="group"
      >
        PLAYGROUND
        <routes.playground.Icon className="text-muted-foreground group-hover:text-foreground trans" />
      </Button>
    </routes.playground.Link>
  );
}
