import { Page } from "@/components/page-layout";
import { ToolCollectionBadge } from "@/components/tools-badge";
import { Card, Cards } from "@/components/ui/card";
import { MoreActions } from "@/components/ui/more-actions";
import { UpdatedAt } from "@/components/updated-at";
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
import { Button } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { useState } from "react";
import { Outlet } from "react-router";
import { CustomToolsEmptyState } from "./CustomToolsEmptyState";
import { MustacheHighlight } from "./ToolBuilder";
import { ToolifyDialog, ToolifyProvider } from "./Toolify";

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
      <Page.Section.CTA>
        <Button onClick={onNewCustomTool}>
          <Button.LeftIcon>
            <Plus className="w-4 h-4" />
          </Button.LeftIcon>
          <Button.Text>New Custom Tool</Button.Text>
        </Button>
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

  return (
    <routes.customTools.toolBuilder.Link
      params={[template.name]}
      className="hover:no-underline"
    >
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
          <ToolCollectionBadge toolNames={template.toolsHint} />
          <UpdatedAt date={new Date(template.updatedAt)} />
        </Card.Footer>
      </Card>
    </routes.customTools.toolBuilder.Link>
  );
}
