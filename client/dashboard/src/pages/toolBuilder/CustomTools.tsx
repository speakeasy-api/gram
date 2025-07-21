import { AddButton } from "@/components/add-button";
import { CreateThingCard } from "@/components/create-thing-card";
import { Page } from "@/components/page-layout";
import { ToolsBadge } from "@/components/tools-badge";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Type } from "@/components/ui/type";
import { HumanizeDateTime } from "@/lib/dates";
import { useRoutes } from "@/routes";
import {
  PromptTemplate,
  PromptTemplateKind,
} from "@gram/client/models/components";
import {
  invalidateAllTemplates,
  useDeleteTemplateMutation,
  useTemplates,
} from "@gram/client/react-query";
import { Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Outlet } from "react-router";
import { CustomToolsEmptyState } from "./CustomToolsEmptyState";
import { MustacheHighlight } from "./ToolBuilder";
import { ToolifyDialog, ToolifyProvider } from "./Toolify";

export function useCustomTools() {
  const { data } = useTemplates();
  return data?.templates.filter(
    (template) => template.kind === PromptTemplateKind.HigherOrderTool
  );
}

export function CustomToolsRoot() {
  return (
    <ToolifyProvider>
      <Outlet />
    </ToolifyProvider>
  );
}

export default function CustomTools() {
  const customTools = useCustomTools();
  const [newToolDialogOpen, setNewToolDialogOpen] = useState(false);

  const onNewCustomTool = () => {
    setNewToolDialogOpen(true);
  };

  let content = <CustomToolsEmptyState onCreateCustomTool={onNewCustomTool} />;

  if (customTools && customTools.length > 0) {
    content = (
      <>
        {customTools?.map((template) => {
          return <CustomToolCard key={template.id} template={template} />;
        })}
        <CreateThingCard onClick={onNewCustomTool}>
          + New Custom Tool
        </CreateThingCard>
      </>
    );
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
        <Page.Header.Actions>
          <AddButton onClick={onNewCustomTool} tooltip="New Custom Tool" />
        </Page.Header.Actions>
      </Page.Header>
      <Page.Body>{content}</Page.Body>
      <ToolifyDialog open={newToolDialogOpen} setOpen={setNewToolDialogOpen} />
    </Page>
  );
}

export function CustomToolCard({ template }: { template: PromptTemplate }) {
  const routes = useRoutes();
  const queryClient = useQueryClient();

  const deleteTemplate = useDeleteTemplateMutation({
    onSuccess: () => {
      invalidateAllTemplates(queryClient);
    },
  });

  let inputsBadge = <Badge variant="secondary">No inputs</Badge>;
  if (template.arguments) {
    const args = JSON.parse(template.arguments);
    const tooltipContent = (
      <Stack gap={1} className="max-h-[300px] overflow-y-auto">
        {Object.keys(args.properties).map((key: string, i: number) => {
          return <p key={i}>{key}</p>;
        })}
      </Stack>
    );
    const numInputs = Object.keys(args.properties).length;
    inputsBadge = (
      <Badge
        variant="secondary"
        tooltip={numInputs > 0 ? tooltipContent : undefined}
      >
        {numInputs} input{numInputs === 1 ? "" : "s"}
      </Badge>
    );
  }

  return (
    <Card>
      <Card.Header>
        <routes.customTools.toolBuilder.Link params={[template.name]}>
          <Card.Title className="normal-case">{template.name}</Card.Title>
        </routes.customTools.toolBuilder.Link>
        <Card.Info>
          <Stack direction="horizontal" gap={2}>
            {inputsBadge}
            <ToolsBadge toolNames={template.toolsHint} />
          </Stack>
        </Card.Info>
        <Card.Description>
          <Stack direction="horizontal" gap={3} justify={"space-between"}>
            {template.description ? (
              <div className="max-w-2/3 line-clamp-3">
                <MustacheHighlight>{template.description}</MustacheHighlight>
              </div>
            ) : null}
            <Type variant="body" muted className="text-sm italic">
              {"Updated "}
              <HumanizeDateTime date={new Date(template.updatedAt)} />
            </Type>
          </Stack>
        </Card.Description>
      </Card.Header>
      <Card.Footer>
        <Button
          variant="destructiveGhost"
          onClick={() => {
            if (confirm("Are you sure you want to delete this tool?")) {
              deleteTemplate.mutate({ request: { name: template.name } });
            }
          }}
          tooltip="Delete this tool"
          icon="trash"
        >
          Delete
        </Button>
        <routes.customTools.toolBuilder.Link params={[template.name]}>
          <Button variant="outline">Edit</Button>
        </routes.customTools.toolBuilder.Link>
      </Card.Footer>
    </Card>
  );
}
