import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { Heading } from "@/components/ui/heading";
import { SkeletonParagraph } from "@/components/ui/skeleton";
import { useProject, useSession } from "@/contexts/Auth";
import { getServerURL } from "@/lib/utils";
import { createOpenRouter } from "@openrouter/ai-sdk-provider";
import { Icon, Stack } from "@speakeasy-api/moonshine";
import { generateObject, UIMessage } from "ai";
import { Loader2 } from "lucide-react";
import { useEffect, useState } from "react";
import { z } from "zod";
import { TS_AGENT_EXAMPLE } from "../sdk/examples";
import { Type } from "@/components/ui/type";

export const Agentify = ({
  projectSlug,
  toolsetSlug,
  environmentSlug,
  messages,
  onAgentify,
}: {
  projectSlug: string;
  toolsetSlug: string;
  environmentSlug: string;
  messages: UIMessage[];
  onAgentify: (agentCode: string) => void;
}) => {
  const session = useSession();
  const project = useProject();

  const [agentifyModalOpen, setAgentifyModalOpen] = useState(false);
  const [agentSummaryPrompt, setAgentSummaryPrompt] = useState<string>();
  const [suggestionNumMessages, setSuggestionNumMessages] = useState(0);
  const [agentifyInProgress, setAgentifyInProgress] = useState(false);

  const openrouter = createOpenRouter({
    apiKey: "this is required",
    baseURL: getServerURL(),
    headers: {
      "Gram-Session": session.session,
      "Gram-Project": project.slug,
    },
  });

  // When the modal is opened, generate a prompt suggestion
  useEffect(() => {
    if (!agentifyModalOpen) return;
    if (suggestionNumMessages === messages.length) return; // Don't generate a new suggestion if the number of messages hasn't changed

    setAgentSummaryPrompt(undefined);

    generateObject({
      model: openrouter.chat("openai/gpt-4o-mini"),
      prompt: `
          <instructions>
            You will be given a chat history. 
            Your job is to distill the user's intent from the chat history to produce a two-sentence prompt explaining the task the user wants to accomplish.
            This prompt will be used to generate an agent that can reusably and extensibly solve the task.
          </instructions>

          <chat-history>
            ${messages.map((m) => `${m.role}: ${m.content}`).join("\n\t")}
          </chat-history>
          `,
      temperature: 0.5,
      schema: z.object({
        promptSuggestion: z.string(),
      }),
    }).then((result) => {
      setAgentSummaryPrompt(result.object.promptSuggestion);
      setSuggestionNumMessages(messages.length);
    });
  }, [agentifyModalOpen]);

  const agentify = async () => {
    setAgentifyInProgress(true);

    const result = await generateObject({
      model: openrouter.chat("openai/gpt-4o-mini"),
      prompt: `
          <instructions>
            You will be given a chat history, a statement of intent, and a basic skeleton of an agent. 
            Using the statement of intent and details from the chat history, produce a complete agent that performs the task.
            The agent should use LLM calls and toolsets to solve the generic version of the task as described in the statement of intent, not just the specific example given.
            Note that the toolset provides tools and handles their execution and authentication. For example, any tool call present in the chat history will be available to the agent.
          </instructions>

          <statement-of-intent>
            ${agentSummaryPrompt}
          </statement-of-intent>
    
          <values-to-use>
            <project>
              ${projectSlug}
            </project>
            <toolset>
              ${toolsetSlug}
            </toolset>
            <environment>
              ${environmentSlug}
            </environment>
          </values-to-use>
          
          <chat-history>
            ${messages.map((m) => `${m.role}: ${m.content}`).join("\n\t")}
          </chat-history>
          
          <example-agent>
            ${TS_AGENT_EXAMPLE}
          </example-agent>
          `,
      temperature: 0.5,
      schema: z.object({
        agentCode: z.string(),
      }),
    });

    onAgentify(result.object.agentCode);
    setAgentifyModalOpen(false);
    setAgentifyInProgress(false);
  };

  const agentifyAvailable = messages.length > 0;
  const agentifyButton = (
    <Button
      variant={"outline"}
      size={"sm"}
      icon="wand-sparkles"
      tooltip={
        "Turn this chat into a reusable agent" +
        (agentifyAvailable ? "" : " (start chatting first)")
      }
      disabled={!agentifyAvailable}
      onClick={() => setAgentifyModalOpen(true)}
    >
      Agentify
    </Button>
  );

  return (
    <>
      {agentifyButton}
      <Dialog open={agentifyModalOpen} onOpenChange={setAgentifyModalOpen}>
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>
              <Stack direction="horizontal" gap={2} align="center">
                <Icon name="wand-sparkles" className="text-muted-foreground" />
                Agentify
              </Stack>
            </Dialog.Title>
            <Dialog.Description>
              Turn this chat into a reusable agent
            </Dialog.Description>
          </Dialog.Header>
          <Stack gap={2}>
            <Heading variant="h5" className="normal-case font-medium">
              {agentSummaryPrompt
                ? "What should the agent do?"
                : "Distilling chat history..."}
            </Heading>
            {agentSummaryPrompt ? (
                <>
              <textarea
                value={agentSummaryPrompt}
                onChange={(e) => setAgentSummaryPrompt(e.target.value)}
                className="w-full h-34 border-2 rounded-lg py-1 px-2"
                disabled={agentifyInProgress}
              />
              <Type muted variant="small" italic>
                The chat history will also be used to generate the agent code.
              </Type>
              </>
            ) : (
              <SkeletonParagraph lines={4} />
            )}
          </Stack>
          <Dialog.Footer>
            <Button variant="ghost" onClick={() => setAgentifyModalOpen(false)}>
              Back
            </Button>
            <Button
              onClick={agentify}
              disabled={!agentSummaryPrompt || agentifyInProgress}
            >
              {agentifyInProgress && (
                <Loader2 className="w-4 h-4 mr-2 animate-spin" />
              )}
              {agentifyInProgress ? "Generating..." : "Agentify"}
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </>
  );
};
