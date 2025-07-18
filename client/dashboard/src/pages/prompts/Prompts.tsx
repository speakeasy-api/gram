import { AddButton } from "@/components/add-button";
import { CreateThingCard } from "@/components/create-thing-card";
import { DeleteButton } from "@/components/delete-button";
import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
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
import { Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Outlet } from "react-router";
import { PromptsEmptyState } from "./PromptsEmptyState";

export function PromptsRoot() {
  return <Outlet />;
}

export function usePrompts() {
  const { data } = useTemplates();
  return data?.templates.filter(
    (template) => template.kind === PromptTemplateKind.Prompt
  );
}

export function getToolsetPrompts(toolset: Toolset | undefined) {
  return toolset?.promptTemplates.filter(
    (template) => template.kind === PromptTemplateKind.Prompt
  );
}

export default function Prompts() {
  const prompts = usePrompts();
  const routes = useRoutes();

  let content = <PromptsEmptyState onCreatePrompt={() => routes.prompts.newPrompt.goTo()} />;

  if (prompts && prompts.length > 0) {
    content = (
      <>
        {prompts?.map((template) => {
          return <PromptTemplateCard key={template.id} template={template} />;
        })}
        <routes.prompts.newPrompt.Link>
          <CreateThingCard>+ New Prompt Template</CreateThingCard>
        </routes.prompts.newPrompt.Link>
      </>
    );
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
        <Page.Header.Actions>
          <routes.prompts.newPrompt.Link>
            <AddButton tooltip="New Prompt Template" />
          </routes.prompts.newPrompt.Link>
        </Page.Header.Actions>
      </Page.Header>
      <Page.Body>{content}</Page.Body>
    </Page>
  );
}

export function PromptTemplateCard({
  template,
  onDelete,
  deleteTooltip,
}: {
  template: PromptTemplate;
  onDelete?: () => void;
  deleteTooltip?: string;
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
        <Card.Title className="normal-case">{template.name}</Card.Title>
        <Card.Description>
          <Stack direction="horizontal" gap={3} justify={"space-between"}>
            {template.description ? (
              <div className="max-w-2/3">{template.description}</div>
            ) : null}
            <Type variant="body" muted className="text-sm italic">
              {"Updated "}
              <HumanizeDateTime date={new Date(template.updatedAt)} />
            </Type>
          </Stack>
        </Card.Description>
      </Card.Header>
      <Card.Footer>
        <Stack direction="horizontal" gap={2}>
          <DeleteButton
            tooltip={deleteTooltip ?? "Delete Prompt Template"}
            onClick={() => {
              if (onDelete) {
                onDelete();
              } else if (
                confirm("Are you sure you want to delete this prompt template?")
              ) {
                deleteTemplate.mutate({ request: { name: template.name } });
              }
            }}
          />
          <routes.prompts.prompt.Link params={[template.name]}>
            <Button variant="outline">Edit</Button>
          </routes.prompts.prompt.Link>
        </Stack>
      </Card.Footer>
    </Card>
  );
}
