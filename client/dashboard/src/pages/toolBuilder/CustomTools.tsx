import { Page } from "@/components/page-layout";
import { Button } from "@speakeasy-api/moonshine";
import { Plus } from "lucide-react";
import { ToolsBadge } from "@/components/tools-badge";
import { Badge } from "@/components/ui/badge";
import { Card, Cards } from "@/components/ui/card";
import { MoreActions } from "@/components/ui/more-actions";
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
import { UpdatedAt } from "@/components/updated-at";

export function useCustomTools() {
  const { data, isLoading } = useTemplates();
  return {
    customTools: data?.templates.filter(
      (template) => template.kind === PromptTemplateKind.HigherOrderTool
    ),
    isLoading,
  };
}

export function CustomToolsRoot() {
  return (
    <ToolifyProvider>
      <Outlet />
    </ToolifyProvider>
  );
}

export default function CustomTools() {
  const { customTools, isLoading } = useCustomTools();
  const [newToolDialogOpen, setNewToolDialogOpen] = useState(false);

  const onNewCustomTool = () => {
    setNewToolDialogOpen(true);
  };

  let content = (
    <Page.Section>
      <Page.Section.Title>Custom Tools</Page.Section.Title>
      <Page.Section.Description>
        Create higher-order tools by sequencing together tools and instructions
      </Page.Section.Description>
      <Page.Section.CTA onClick={onNewCustomTool}>
        <Button.LeftIcon>
          <Plus className="w-4 h-4" />
        </Button.LeftIcon>
        <Button.Text>New Custom Tool</Button.Text>
      </Page.Section.CTA>
      <Page.Section.Body>
        <Cards isLoading={isLoading}>
          {customTools?.map((template) => {
            return <CustomToolCard key={template.id} template={template} />;
          })}
        </Cards>
      </Page.Section.Body>
    </Page.Section>
  );

  if (!isLoading && (!customTools || customTools.length === 0)) {
    content = <CustomToolsEmptyState onCreateCustomTool={onNewCustomTool} />;
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
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
        variant="outline"
        tooltip={numInputs > 0 ? tooltipContent : undefined}
      >
        {numInputs > 0 ? numInputs : "No"} input{numInputs === 1 ? "" : "s"}
      </Badge>
    );
  }

  return (
    <routes.customTools.toolBuilder.Link params={[template.name]} className="hover:no-underline">
      <Card>
        <Card.Header>
          <Card.Title className="normal-case">{template.name}</Card.Title>
          <MoreActions
            actions={[
              {
                label: "Delete",
                destructive: true,
                icon: "trash",
                onClick: () => {
                  if (confirm("Are you sure you want to delete this tool?")) {
                    deleteTemplate.mutate({ request: { name: template.name } });
                  }
                },
              },
            ]}
          />
        </Card.Header>
        <Card.Content>
          {template.description && (
            <Card.Description>
              <div className="line-clamp-3">
                <MustacheHighlight>{template.description}</MustacheHighlight>
              </div>
            </Card.Description>
          )}
        </Card.Content>
        <Card.Footer>
          <Stack direction="horizontal" gap={1}>
            {inputsBadge}
            <ToolsBadge toolNames={template.toolsHint} />
          </Stack>
          <UpdatedAt date={new Date(template.updatedAt)} />
        </Card.Footer>
      </Card>
    </routes.customTools.toolBuilder.Link>
  );
}
