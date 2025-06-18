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
  const routes = useRoutes();
  const [newToolDialogOpen, setNewToolDialogOpen] = useState(false);

  const onNewCustomTool = () => {
    routes.customTools.toolBuilderNew.goTo();
  };

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
        <Page.Header.Actions>
          <AddButton onClick={onNewCustomTool} tooltip="New Custom Tool" />
        </Page.Header.Actions>
      </Page.Header>
      <Page.Body>
        {customTools?.map((template) => {
          return <CustomToolCard key={template.id} template={template} />;
        })}
        <CreateThingCard onClick={() => setNewToolDialogOpen(true)}>
          + New Custom Tool
        </CreateThingCard>
        <ToolifyDialog
          open={newToolDialogOpen}
          setOpen={setNewToolDialogOpen}
        />
      </Page.Body>
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
        <Stack direction="horizontal" gap={2} justify={"space-between"}>
          <Card.Title className="normal-case">{template.name}</Card.Title>
          <Stack direction="horizontal" gap={2}>
            {inputsBadge}
            <ToolsBadge toolNames={template.toolsHint} />
          </Stack>
        </Stack>
        <Stack direction="horizontal" gap={3} justify={"space-between"}>
          {template.description ? (
            <Card.Description className="max-w-2/3 line-clamp-3">
              <MustacheHighlight>{template.description}</MustacheHighlight>
            </Card.Description>
          ) : null}
          <Type variant="body" muted className="text-sm italic">
            {"Updated "}
            <HumanizeDateTime date={new Date(template.updatedAt)} />
          </Type>
        </Stack>
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
