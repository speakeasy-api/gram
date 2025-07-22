import { Page } from "@/components/page-layout";
import { Card, Cards } from "@/components/ui/card";
import { MoreActions } from "@/components/ui/more-actions";
import { Type } from "@/components/ui/type";
import { HumanizeDateTime } from "@/lib/dates";
import { useRoutes } from "@/routes";
import {
  PromptTemplate,
  PromptTemplateKind,
  Toolset,
} from "@gram/client/models/components";
import {
  invalidateAllTemplates,
  useDeleteTemplateMutation,
  useTemplates,
} from "@gram/client/react-query/index.js";
import { useQueryClient } from "@tanstack/react-query";
import { Outlet } from "react-router";
import { PromptsEmptyState } from "./PromptsEmptyState";

export function PromptsRoot() {
  return <Outlet />;
}

export function usePrompts() {
  const { data, isLoading } = useTemplates();
  return {
    prompts: data?.templates.filter(
      (template) => template.kind === PromptTemplateKind.Prompt
    ),
    isLoading,
  };
}

export function getToolsetPrompts(toolset: Toolset | undefined) {
  return toolset?.promptTemplates.filter(
    (template) => template.kind === PromptTemplateKind.Prompt
  );
}

export default function Prompts() {
  const { prompts, isLoading } = usePrompts();
  const routes = useRoutes();

  let content = (
    <PromptsEmptyState onCreatePrompt={() => routes.prompts.newPrompt.goTo()} />
  );

  if (prompts && prompts.length > 0) {
    content = (
      <Page.Section>
        <Page.Section.Title>Prompt Templates</Page.Section.Title>
        <Page.Section.Description>
          Provide your users with MCP-native prompt templates
        </Page.Section.Description>
        <Page.Section.CTA
          onClick={() => routes.prompts.newPrompt.goTo()}
          icon="plus"
        >
          New Prompt Template
        </Page.Section.CTA>
        <Page.Section.Body>
          <Cards isLoading={isLoading}>
            {prompts?.map((template) => {
              return (
                <PromptTemplateCard key={template.id} template={template} />
              );
            })}
          </Cards>
        </Page.Section.Body>
      </Page.Section>
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
    <Card>
      <Card.Header>
        <routes.prompts.prompt.Link params={[template.name]}>
          <Card.Title className="normal-case">{template.name}</Card.Title>
        </routes.prompts.prompt.Link>
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
                    "Are you sure you want to delete this prompt template?"
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
        <Type variant="body" muted className="text-sm italic">
          {"Updated "}
          <HumanizeDateTime date={new Date(template.updatedAt)} />
        </Type>
      </Card.Footer>
    </Card>
  );
}
