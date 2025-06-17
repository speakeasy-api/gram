import { CopyableSlug } from "@/components/name-and-slug";
import {
  ToolsetPromptsBadge,
  ToolsetToolsBadge,
} from "@/components/tools-badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Type } from "@/components/ui/type";
import { HumanizeDateTime } from "@/lib/dates";
import { useRoutes } from "@/routes";
import { Toolset } from "@gram/client/models/components";
import { Stack } from "@speakeasy-api/moonshine";

export function ToolsetCard({
  toolset,
  className,
}: {
  toolset: Toolset;
  className?: string;
}) {
  const routes = useRoutes();

  return (
    <Card className={className}>
      <Card.Header>
        <Stack direction="horizontal" gap={2} justify={"space-between"}>
          <Card.Title>
            <CopyableSlug slug={toolset.slug}>
              <routes.toolsets.toolset.Link params={[toolset.slug]}>
                {toolset.name}
              </routes.toolsets.toolset.Link>
            </CopyableSlug>
          </Card.Title>
          <Stack direction="horizontal" gap={1} align="center">
            <ToolsetPromptsBadge toolset={toolset} />
            <ToolsetToolsBadge toolset={toolset} />
          </Stack>
        </Stack>
        <Stack direction="horizontal" gap={3} justify={"space-between"}>
          <Card.Description className="max-w-2/3">
            {toolset.description}
          </Card.Description>
          <Type variant="body" muted className="text-sm italic">
            {"Updated "}
            <HumanizeDateTime date={new Date(toolset.updatedAt)} />
          </Type>
        </Stack>
      </Card.Header>
      <Card.Footer>
        <Stack direction="horizontal" gap={2}>
          <ToolsetPlaygroundLink toolset={toolset} />
          <routes.toolsets.toolset.Link params={[toolset.slug]}>
            <Button variant="outline">Edit</Button>
          </routes.toolsets.toolset.Link>
        </Stack>
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
      >
        Playground
        <routes.playground.Icon className="text-muted-foreground group-hover:text-foreground trans" />
      </Button>
    </routes.playground.Link>
  );
}
