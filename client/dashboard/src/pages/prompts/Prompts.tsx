import { Page } from "@/components/page-layout";
import {
  useTemplatesSuspense,
  useToolsetSuspense,
} from "@gram/client/react-query/index.js";
import { Outlet, useParams } from "react-router";
import { AddButton } from "@/components/add-button";
import { Card } from "@/components/ui/card";
import { PromptTemplate } from "@gram/client/models/components";
import { Stack } from "@speakeasy-api/moonshine";
import { useRoutes } from "@/routes";
import { Type } from "@/components/ui/type";
import { HumanizeDateTime } from "@/lib/dates";
import { Button } from "@/components/ui/button";

export const useToolset = () => {
  const { toolsetSlug } = useParams();

  const { data: toolset, refetch: refetchToolset } = useToolsetSuspense({
    slug: toolsetSlug ?? "",
  });

  return Object.assign(toolset, { refetch: refetchToolset });
};

export function PromptsRoot() {
  return <Outlet />;
}

export default function Prompts() {
  const { data } = useTemplatesSuspense();
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
        {data.templates.map((template) => {
          return <PromptTemplateCard key={template.id} template={template} />;
        })}
      </Page.Body>
    </Page>
  );
}

function PromptTemplateCard({ template }: { template: PromptTemplate }) {
  const routes = useRoutes();

  return (
    <Card>
      <Card.Header>
        <Stack direction="horizontal" gap={2} justify={"space-between"}>
          <Card.Title className="normal-case">{template.name}</Card.Title>
        </Stack>
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
      </Card.Header>
      <Card.Content>
        <div className="flex items-center justify-between w-full">
          <div className="flex items-center gap-2">
            <routes.prompts.prompt.Link params={[template.name]}>
              <Button variant="outline">Edit</Button>
            </routes.prompts.prompt.Link>
          </div>
        </div>
      </Card.Content>
    </Card>
  );
}
