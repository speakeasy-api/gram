import { TopUpCTA, UsageProgress } from "@/components/billing/usage-controls";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Card, Cards } from "@/components/ui/card";
import { MoreActions } from "@/components/ui/more-actions";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { UpdatedAt } from "@/components/updated-at";
import { useProductTier } from "@/hooks/useProductTier";
import { useRoutes } from "@/routes";
import { Assistant } from "@gram/client/models/components/assistant.js";
import {
  invalidateAllAssistantsList,
  useAssistantsDeleteMutation,
  useAssistantsList,
  useGetPeriodUsage,
} from "@gram/client/react-query/index.js";
import { Button, Icon, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Info, Plus } from "lucide-react";
import { Outlet } from "react-router";

export function AssistantsRoot() {
  return <Outlet />;
}

function StatusBadge({ status }: { status: string }) {
  if (status === "active") return <Badge variant="default">Active</Badge>;
  if (status === "paused") return <Badge variant="secondary">Paused</Badge>;
  return <Badge variant="secondary">{status}</Badge>;
}

function AssistantsEmptyState({ onCreate }: { onCreate: () => void }) {
  return (
    <div className="bg-muted/20 flex flex-col items-center justify-center rounded-xl border border-dashed px-8 py-16">
      <div className="bg-muted/50 mb-4 flex h-12 w-12 items-center justify-center rounded-full">
        <Icon name="bot" className="text-muted-foreground h-6 w-6" />
      </div>
      <Type variant="subheading" className="mb-1">
        No assistants yet
      </Type>
      <Type small muted className="mb-4 max-w-md text-center">
        Create an assistant to wire a model up to your MCP servers.
      </Type>
      <Button onClick={onCreate}>
        <Button.LeftIcon>
          <Plus className="h-4 w-4" />
        </Button.LeftIcon>
        <Button.Text>Create Assistant</Button.Text>
      </Button>
    </div>
  );
}

export default function AssistantsIndex() {
  const routes = useRoutes();
  const { data, isLoading } = useAssistantsList(undefined, undefined, {
    retry: false,
    throwOnError: false,
  });

  const assistants = data?.assistants ?? [];

  const content =
    !isLoading && assistants.length === 0 ? (
      <AssistantsEmptyState
        onCreate={() => routes.assistants.newAssistant.goTo()}
      />
    ) : (
      <Page.Section>
        <Page.Section.Title>Assistants</Page.Section.Title>
        <Page.Section.Description>
          Configure model, instructions, and MCP servers for each assistant.
        </Page.Section.Description>
        <Page.Section.CTA>
          <Button onClick={() => routes.assistants.newAssistant.goTo()}>
            <Button.LeftIcon>
              <Plus className="h-4 w-4" />
            </Button.LeftIcon>
            <Button.Text>New Assistant</Button.Text>
          </Button>
        </Page.Section.CTA>
        <Page.Section.Body>
          {isLoading ? (
            <Stack align="center" justify="center" className="py-16">
              <Icon
                name="loader-circle"
                className="text-muted-foreground h-6 w-6 animate-spin"
              />
            </Stack>
          ) : (
            <Cards>
              {assistants.map((assistant) => (
                <AssistantCard key={assistant.id} assistant={assistant} />
              ))}
            </Cards>
          )}
        </Page.Section.Body>
      </Page.Section>
    );

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <UsageSection />
        {content}
      </Page.Body>
    </Page>
  );
}

function UsageSection() {
  const productTier = useProductTier();
  const { data: periodUsage, isError } = useGetPeriodUsage(
    undefined,
    undefined,
    { throwOnError: false },
  );

  if (isError) return null;

  return (
    <Page.Section>
      <Page.Section.Title>Assistant Credits</Page.Section.Title>
      <Page.Section.Description>
        Credits consumed by assistant runs this billing period. Each turn debits
        credits based on the underlying model's cost.
      </Page.Section.Description>
      <RequireScope scope="org:admin" level="section">
        <TopUpCTA />
      </RequireScope>
      <Page.Section.Body>
        <Stack gap={3} className="mb-6">
          <Stack direction="horizontal" align="center" gap={1}>
            <Type variant="body" className="font-medium">
              Credits
            </Type>
            <SimpleTooltip tooltip="Credits track model usage across assistants and chat. 1 credit ≈ $1 of model cost.">
              <Info className="text-muted-foreground h-4 w-4" />
            </SimpleTooltip>
          </Stack>
          {periodUsage ? (
            <UsageProgress
              value={periodUsage.credits}
              included={periodUsage.includedCredits || 1}
              overageIncrement={periodUsage.includedCredits || 1}
              noMax={productTier === "enterprise"}
            />
          ) : (
            <Skeleton className="h-4 w-full" />
          )}
        </Stack>
      </Page.Section.Body>
    </Page.Section>
  );
}

function AssistantCard({ assistant }: { assistant: Assistant }) {
  const routes = useRoutes();
  const queryClient = useQueryClient();

  const deleteAssistant = useAssistantsDeleteMutation({
    onSuccess: () => {
      invalidateAllAssistantsList(queryClient);
    },
  });

  const toolsetLabel = `${assistant.toolsets.length} MCP server${
    assistant.toolsets.length !== 1 ? "s" : ""
  }`;

  return (
    <routes.assistants.detail.Link
      params={[assistant.id]}
      className="hover:no-underline"
    >
      <Card>
        <Card.Header>
          <Card.Title className="normal-case">{assistant.name}</Card.Title>
          <MoreActions
            actions={[
              {
                label: "Delete",
                destructive: true,
                icon: "trash",
                onClick: () => {
                  if (confirm(`Delete assistant "${assistant.name}"?`)) {
                    deleteAssistant.mutate({ request: { id: assistant.id } });
                  }
                },
              },
            ]}
          />
        </Card.Header>
        <Card.Content>
          <Card.Description>
            <Stack direction="horizontal" gap={2} align="center">
              <StatusBadge status={assistant.status} />
              <Type muted small>
                {assistant.model}
              </Type>
            </Stack>
          </Card.Description>
        </Card.Content>
        <Card.Footer>
          <Type muted small>
            {toolsetLabel}
          </Type>
          <UpdatedAt date={new Date(assistant.updatedAt)} />
        </Card.Footer>
      </Card>
    </routes.assistants.detail.Link>
  );
}
