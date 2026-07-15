import { Page } from "@/components/page-layout";
import { ListLayout } from "@/components/layouts/list-layout";
import { RequireScope } from "@/components/require-scope";
import { CardContextMenu } from "@/components/card-context-menu";
import { Card, Cards } from "@/components/ui/card";
import { useConfirm } from "@/components/ui/use-confirm";
import { Action, MoreActions } from "@/components/ui/more-actions";
import { UpdatedAt } from "@/components/updated-at";
import { useRoutes } from "@/routes";
import { PromptTemplate } from "@gram/client/models/components/prompttemplate.js";
import { useDeleteTemplateMutation } from "@gram/client/react-query/deleteTemplate.js";
import { invalidateAllTemplates } from "@gram/client/react-query/templates.js";
import { usePrompts } from "./usePrompts";
import { Button } from "@/components/ui/button";
import { useQueryClient } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { Outlet } from "react-router";
import { PromptsEmptyState } from "./PromptsEmptyState";

export function PromptsRoot(): JSX.Element {
  return <Outlet />;
}

export default function Prompts(): JSX.Element {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope={["project:read", "project:write"]} level="page">
          <PromptsInner />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}

function PromptsInner() {
  const { prompts, isLoading } = usePrompts();
  const routes = useRoutes();

  if (!isLoading && prompts && prompts.length === 0) {
    return (
      <PromptsEmptyState
        onCreatePrompt={() => routes.prompts.newPrompt.goTo()}
      />
    );
  }

  return (
    <ListLayout>
      <ListLayout.Header
        title="Prompt Templates"
        subtitle="Provide your users with MCP-native prompt templates"
        actions={
          <Button onClick={() => routes.prompts.newPrompt.goTo()}>
            <Button.LeftIcon>
              <Plus className="h-4 w-4" />
            </Button.LeftIcon>
            <Button.Text>New Prompt</Button.Text>
          </Button>
        }
      />
      <ListLayout.List>
        <Cards isLoading={isLoading}>
          {prompts?.map((template) => {
            return <PromptTemplateCard key={template.id} template={template} />;
          })}
        </Cards>
      </ListLayout.List>
    </ListLayout>
  );
}

export function PromptTemplateCard({
  template,
  onDelete,
  deleteLabel,
}: {
  template: PromptTemplate;
  onDelete?: () => void;
  deleteLabel?: string;
}): JSX.Element {
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
      title: "Are you sure you want to delete this prompt template?",
      destructive: true,
    });
    if (confirmed) {
      deleteTemplate.mutate({ request: { name: template.name } });
    }
  };

  const actions: Action[] = [
    {
      label: deleteLabel ?? "Delete",
      destructive: true,
      icon: "trash",
      onClick: () => {
        if (onDelete) {
          onDelete();
        } else {
          void handleDelete();
        }
      },
    },
  ];

  return (
    <>
      <CardContextMenu actions={actions}>
        <routes.prompts.prompt.Link
          params={[template.name]}
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
              <Card.Description>
                {template.description || "No description"}
              </Card.Description>
            </Card.Content>
            <Card.Footer>
              <div />
              <UpdatedAt date={new Date(template.updatedAt)} />
            </Card.Footer>
          </Card>
        </routes.prompts.prompt.Link>
      </CardContextMenu>
      {dialog}
    </>
  );
}
