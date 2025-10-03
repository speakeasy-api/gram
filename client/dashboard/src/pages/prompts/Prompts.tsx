import { Page } from "@/components/page-layout";
import { Card, Cards } from "@/components/ui/card";
import { MoreActions } from "@/components/ui/more-actions";
import { UpdatedAt } from "@/components/updated-at";
import { isPrompt } from "@/lib/toolTypes";
import { useRoutes } from "@/routes";
import { PromptTemplate } from "@gram/client/models/components";
import {
  invalidateAllTemplates,
  useDeleteTemplateMutation,
  useTemplates,
} from "@gram/client/react-query/index.js";
import { Button } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { Outlet } from "react-router";
import { PromptsEmptyState } from "./PromptsEmptyState";

export function PromptsRoot() {
  return <Outlet />;
}

export function usePrompts() {
  const { data, isLoading } = useTemplates();
  return {
    prompts: data?.templates.filter(isPrompt),
    isLoading,
  };
}

export default function Prompts() {
  const { prompts, isLoading } = usePrompts();
  const routes = useRoutes();

  let content = (
    <Page.Section>
      <Page.Section.Title>Prompt Templates</Page.Section.Title>
      <Page.Section.Description>
        Provide your users with MCP-native prompt templates
      </Page.Section.Description>
      <Page.Section.CTA>
        <Button onClick={() => routes.prompts.newPrompt.goTo()}>
          <Button.LeftIcon>
            <Plus className="w-4 h-4" />
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

  if (!isLoading && prompts && prompts.length === 0) {
    content = (
      <PromptsEmptyState
        onCreatePrompt={() => routes.prompts.newPrompt.goTo()}
      />
    );
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>{content}</Page.Body>
    </Page>
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
}) {
  const routes = useRoutes();
  const queryClient = useQueryClient();

  const deleteTemplate = useDeleteTemplateMutation({
    onSuccess: () => {
      invalidateAllTemplates(queryClient);
    },
  });

  return (
    <routes.prompts.prompt.Link
      params={[template.name]}
      className="hover:no-underline"
    >
      <Card>
        <Card.Header>
          <Card.Title className="normal-case">{template.name}</Card.Title>
          <MoreActions
            actions={[
              {
                label: deleteLabel ?? "Delete",
                destructive: true,
                icon: "trash",
                onClick: () => {
                  if (onDelete) {
                    onDelete();
                  } else if (
                    confirm(
                      "Are you sure you want to delete this prompt template?",
                    )
                  ) {
                    deleteTemplate.mutate({ request: { name: template.name } });
                  }
                },
              },
            ]}
          />
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
  );
}
