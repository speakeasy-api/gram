import { AddButton } from "@/components/add-button";
import { CreateThingCard } from "@/components/create-thing-card";
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
import { useTemplates } from "@gram/client/react-query/index.js";
import { Stack } from "@speakeasy-api/moonshine";
import { Outlet } from "react-router";

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

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
        <Page.Header.Actions>
          <AddButton
            onClick={() => routes.prompts.newPrompt.goTo()}
            tooltip="New Prompt Template"
          />
        </Page.Header.Actions>
      </Page.Header>
      <Page.Body>
        {prompts?.map((template) => {
          return <PromptTemplateCard key={template.id} template={template} />;
        })}
        <CreateThingCard onClick={() => routes.prompts.newPrompt.goTo()}>
          + New Prompt Template
        </CreateThingCard>
      </Page.Body>
    </Page>
  );
}

export function PromptTemplateCard({
  template,
  actions,
}: {
  template: PromptTemplate;
  actions?: React.ReactNode;
}) {
  const routes = useRoutes();

  return (
    <Card>
      <Card.Header>
        <Card.Title className="normal-case">{template.name}</Card.Title>
        <Stack direction="horizontal" gap={3} justify={"space-between"}>
          {template.description ? (
            <Card.Description className="max-w-2/3">
              {template.description}
            </Card.Description>
          ) : null}
          <Type variant="body" muted className="text-sm italic">
            {"Updated "}
            <HumanizeDateTime date={new Date(template.updatedAt)} />
          </Type>
        </Stack>
        {actions && <Card.Actions>{actions}</Card.Actions>}
      </Card.Header>
      <Card.Content>
        <routes.prompts.prompt.Link params={[template.name]}>
          <Button variant="outline">Edit</Button>
        </routes.prompts.prompt.Link>
      </Card.Content>
    </Card>
  );
}
