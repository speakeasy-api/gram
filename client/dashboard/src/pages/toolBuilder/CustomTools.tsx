import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { CardContextMenu } from "@/components/card-context-menu";
import { ToolCollectionBadge } from "@/components/tool-collection-badge";
import { Card, Cards } from "@/components/ui/card";
import { useConfirm } from "@/components/ui/use-confirm";
import { Action, MoreActions } from "@/components/ui/more-actions";
import { UpdatedAt } from "@/components/updated-at";
import { useRoutes } from "@/routes";
import { PromptTemplate } from "@gram/client/models/components/prompttemplate.js";
import { useDeleteTemplateMutation } from "@gram/client/react-query/deleteTemplate.js";
import { invalidateAllTemplates } from "@gram/client/react-query/templates.js";
import { Button } from "@/components/ui/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { useState } from "react";
import { Outlet } from "react-router";
import { CustomToolsEmptyState } from "./CustomToolsEmptyState";
import { MustacheHighlight } from "./ToolBuilder";
import { ToolifyDialog, ToolifyProvider } from "./Toolify";
import { useCustomTools } from "./useCustomTools";

export function CustomToolsRoot(): JSX.Element {
  return (
    <ToolifyProvider>
      <Outlet />
    </ToolifyProvider>
  );
}

export default function CustomTools(): JSX.Element {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope={["project:read", "project:write"]} level="page">
          <CustomToolsInner />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}

function CustomToolsInner() {
  const { customTools, isLoading } = useCustomTools();
  const [newToolDialogOpen, setNewToolDialogOpen] = useState(false);

  const onNewCustomTool = () => {
    setNewToolDialogOpen(true);
  };

  return (
    <>
      {!isLoading && (!customTools || customTools.length === 0) ? (
        <CustomToolsEmptyState onCreateCustomTool={onNewCustomTool} />
      ) : (
        <Page.Section>
          <Page.Section.Title>Custom Tools</Page.Section.Title>
          <Page.Section.Description>
            Create higher-order tools by sequencing together tools and
            instructions
          </Page.Section.Description>
          <Page.Section.CTA>
            <Button onClick={onNewCustomTool}>
              <Button.LeftIcon>
                <Plus className="h-4 w-4" />
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
      )}
      <ToolifyDialog open={newToolDialogOpen} setOpen={setNewToolDialogOpen} />
    </>
  );
}

function CustomToolCard({ template }: { template: PromptTemplate }) {
  const routes = useRoutes();
  const queryClient = useQueryClient();
  const { confirm: requestConfirm, dialog } = useConfirm();

  const deleteTemplate = useDeleteTemplateMutation({
    onSuccess: () => {
      void invalidateAllTemplates(queryClient);
    },
  });

  const handleDelete = async () => {
    const confirmed = await requestConfirm({
      title: "Are you sure you want to delete this tool?",
      destructive: true,
    });
    if (confirmed) {
      deleteTemplate.mutate({ request: { name: template.name } });
    }
  };

  const actions: Action[] = [
    {
      label: "Delete",
      destructive: true,
      icon: "trash",
      onClick: () => void handleDelete(),
    },
  ];

  return (
    <>
      <CardContextMenu actions={actions}>
        <routes.customTools.toolBuilder.Link
          params={[template.canonicalName]}
          className="block h-full hover:no-underline"
        >
          <Card>
            <Card.Header>
              <Card.Title className="normal-case">{template.name}</Card.Title>
              <div onClick={(e) => e.stopPropagation()}>
                <MoreActions actions={actions} />
              </div>
            </Card.Header>
            <Card.Content>
              {template.description && (
                <Card.Description className="line-clamp-3 whitespace-normal">
                  {template.description}
                  <MustacheHighlight>{template.description}</MustacheHighlight>
                </Card.Description>
              )}
            </Card.Content>
            <Card.Footer>
              <ToolCollectionBadge toolNames={template.toolsHint} />
              <UpdatedAt date={new Date(template.updatedAt)} />
            </Card.Footer>
          </Card>
        </routes.customTools.toolBuilder.Link>
      </CardContextMenu>
      {dialog}
    </>
  );
}
