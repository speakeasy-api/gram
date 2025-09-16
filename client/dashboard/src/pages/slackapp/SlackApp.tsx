import { Page } from "@/components/page-layout";
import { Button } from "@speakeasy-api/moonshine";
import { Dialog } from "@/components/ui/dialog";
import { useGetSlackConnection } from "@gram/client/react-query";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useSdkClient } from "@/contexts/Sdk";
import { useState } from "react";
import { getServerURL } from "@/lib/utils";
import { useToolsets } from "../toolsets/Toolsets";
import { Card } from "@/components/ui/card";
import { Stack, Icon } from "@speakeasy-api/moonshine";
import { Type } from "@/components/ui/type";
import { HumanizeDateTime } from "@/lib/dates";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@/components/ui/tooltip";
import { InfoIcon } from "lucide-react";
import { useSearchParams } from "react-router";
import { useProject } from "@/contexts/Auth";

export default function SlackApp() {
  const client = useSdkClient();
  const queryClient = useQueryClient();
  const { data, isLoading } = useGetSlackConnection(undefined, undefined, {
    refetchOnWindowFocus: false,
    retry: false,
  });
  const [modalOpen, setModalOpen] = useState(false);
  const [toolset, setToolset] = useState("");
  const [connectionDeleted, setConnectionDeleted] = useState(false);

  const toolsets = useToolsets();
  const project = useProject();

  const [searchParams] = useSearchParams();
  const slackError = searchParams.get("slack_error");

  const updateMutation = useMutation({
    mutationFn: async (newToolset: string) => {
      return client.slack.updateSlackConnection({
        updateSlackConnectionRequestBody: { defaultToolsetSlug: newToolset },
      });
    },
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ["@gram/client", "slack", "getSlackConnection"],
      });
      setModalOpen(false);
    },
  });

  const deleteMutation = useMutation({
    mutationFn: async () => {
      return client.slack.deleteSlackConnection();
    },
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ["@gram/client", "slack", "getSlackConnection"],
      });
      setConnectionDeleted(true);
    },
  });

  if (isLoading) {
    return (
      <Page>
        <Page.Body>Loading...</Page.Body>
      </Page>
    );
  }
  // If there is any error, just fall through and show the install button (treat as not installed)

  const installed = !!data && !connectionDeleted;

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <div className="flex items-start justify-between mb-1">
          <h2>
            Perform agent tasks with Gram toolsets using the Gram Slack App
            {installed && (
              <Tooltip>
                <TooltipTrigger asChild>
                  <span tabIndex={0} className="cursor-pointer">
                    <InfoIcon className="w-4 h-4 text-muted-foreground" />
                  </span>
                </TooltipTrigger>
                <TooltipContent side="right" className="max-w-xs">
                  <div className="text-muted-foreground text-sm whitespace-pre whitespace-nowrap overflow-x-auto">
                    {`- Invoke @Gram in a DM, channel, or thread
- @Gram list to see a list of available toolsets
- @Gram (toolset-slug) ... to invoke a specific toolset`}
                  </div>
                </TooltipContent>
              </Tooltip>
            )}
          </h2>
          {!installed && (
            <Button
              onClick={() => {
                window.location.href = `${getServerURL()}/rpc/${project.slug}/slack.login?return_url=${encodeURIComponent(
                  window.location.href
                )}`;
              }}
              variant="secondary"
              size="md"
              className="px-6 py-2 rounded-lg"
            >
              Connect Slack
            </Button>
          )}
        </div>
        {slackError && (
          <p className="text-red-600 text-left">
            Error: {decodeURIComponent(slackError)}
          </p>
        )}
        {!installed && (
          <div className="bg-muted border border-border rounded-md p-4 mb-4">
            <div className="text-muted-foreground mb-2">
              How to use the Gram slack app in your workspace
            </div>
            <ul className="list-disc pl-6 text-muted-foreground text-sm">
              <li>Message with Gram directly in a DM</li>
              <li>Mention @Gram in any channel or thread to start chatting</li>
              <li>@Gram list to see a list of available toolsets</li>
              <li>@Gram (toolset-slug) ... to invoke a specific toolset</li>
            </ul>
          </div>
        )}
        {installed && (
          <Card>
            <Card.Header>
              <Stack direction="horizontal" gap={2} align="center">
                <span className="w-8 h-8 rounded-md bg-green-500 flex items-center justify-center">
                  <Icon name="slack" className="w-8 h-8 text-white" />
                </span>
                <Card.Title>Slack Workspace ({data.slackTeamName})</Card.Title>
              </Stack>
              <Stack direction="horizontal" gap={3} justify="space-between">
                <Card.Description className="max-w-2/3">
                  Default toolset: {data.defaultToolsetSlug || "not set"}
                </Card.Description>
                <Type variant="body" muted className="text-sm italic">
                  {"Created "}
                  <HumanizeDateTime date={new Date(data.createdAt)} />
                </Type>
              </Stack>
            </Card.Header>
            <Card.Footer>
              <Button variant="secondary"
                onClick={() => {
                  setToolset(data.defaultToolsetSlug || "");
                  setModalOpen(true);
                }}
              >
                Change Default Toolset
              </Button>
              <Button
                variant="destructive-secondary"
                onClick={() => deleteMutation.mutate()}
                disabled={deleteMutation.isPending}
              >
                DELETE
              </Button>
            </Card.Footer>
            <Dialog open={modalOpen} onOpenChange={setModalOpen}>
              <Dialog.Content>
                <Dialog.Header>
                  <Dialog.Title>Change Default Toolset</Dialog.Title>
                </Dialog.Header>
                <div className="flex flex-col gap-4 mt-4">
                  <select
                    className="border rounded px-2 py-1"
                    value={toolset}
                    onChange={(e) => setToolset(e.target.value)}
                  >
                    <option value="" disabled>
                      Select a toolset
                    </option>
                    {toolsets.map((ts) => (
                      <option key={ts.slug} value={ts.slug}>
                        {ts.slug}
                      </option>
                    ))}
                  </select>
                  <div className="flex gap-2 justify-end">
                    <Button variant="tertiary" onClick={() => setModalOpen(false)}>
                      Cancel
                    </Button>
                    <Button
                      onClick={() => updateMutation.mutate(toolset)}
                      disabled={updateMutation.isPending || !toolset}
                    >
                      Save
                    </Button>
                  </div>
                </div>
              </Dialog.Content>
            </Dialog>
          </Card>
        )}
      </Page.Body>
    </Page>
  );
}
