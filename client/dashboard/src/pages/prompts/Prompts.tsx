import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { CardContextMenu } from "@/components/card-context-menu";
import { Card, Cards } from "@/components/ui/card";
import { Action, MoreActions } from "@/components/ui/more-actions";
import { UpdatedAt } from "@/components/updated-at";
import { useRoutes } from "@/routes";
import { PromptTemplate } from "@gram/client/models/components/prompttemplate.js";
import { useDeleteTemplateMutation } from "@gram/client/react-query/deleteTemplate.js";
import { invalidateAllTemplates } from "@gram/client/react-query/templates.js";
import { usePrompts } from "./usePrompts";
import { Button } from "@/components/ui/moonshine";
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
    <Page.Section>
      <Page.Section.Title>Prompt Templates</Page.Section.Title>
      <Page.Section.Description>
        Provide your users with MCP-native prompt templates
      </Page.Section.Description>
      <Page.Section.CTA>
        <Button onClick={() => routes.prompts.newPrompt.goTo()}>
          <Button.LeftIcon>
            <Plus className="h-4 w-4" />
          </Button.LeftIcon>
          <Button.Text>New Prompt</Button.Text>
        </Button>
      </Page.Section.CTA>
      <Page.Section.Body>
        <Cards isLoading={isLoading}>
          {prompts?.map((template) => {
            return <PromptTemplateCard key={template.id} template={template} />;
          })}
        </Cards>
      </Page.Section.Body>
    </Page.Section>
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

  const deleteTemplate = useDeleteTemplateMutation({
    onSuccess: () => {
      void invalidateAllTemplates(queryClient);
    },
  });

  const actions: Action[] = [
    {
      label: deleteLabel ?? "Delete",
      destructive: true,
      icon: "trash",
      onClick: () => {
        if (onDelete) {
          onDelete();
        } else if (
          confirm("Are you sure you want to delete this prompt template?")
        ) {
          deleteTemplate.mutate({ request: { name: template.name } });
        }
      },
    },
  ];

  return (
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
  );
}
