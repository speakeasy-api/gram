import { Button } from "@speakeasy-api/moonshine";
import { Dialog } from "@/components/ui/dialog";
import { Heading } from "@/components/ui/heading";
import { SkeletonParagraph } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { TextArea } from "@/components/ui/textarea";
import { Type } from "@/components/ui/type";
import { useProject, useSession } from "@/contexts/Auth";
import { useTelemetry } from "@/contexts/Telemetry";
import { getServerURL } from "@/lib/utils";
import { createOpenRouter } from "@openrouter/ai-sdk-provider";
import { Icon, Stack } from "@speakeasy-api/moonshine";
import { generateObject } from "ai";
import { createContext, useContext, useEffect, useState } from "react";
import { z } from "zod";
import {
  AGENT_EXAMPLES,
  FRAMEWORKS,
  OPENAI_AGENTS_SDK,
  SdkFramework,
  SdkLanguage,
} from "../sdk/examples";
import { SdkLanguageDropdown } from "../sdk/SDK";
import { useChatMessages } from "./ChatContext";
import { useMiniModel } from "./Openrouter";

export const useAgentify = () => {
  return useContext(AgentifyContext);
};

const AgentifyContext = createContext<{
  lang: SdkLanguage;
  setLang: (lang: SdkLanguage) => void;
  framework: SdkFramework;
  setFramework: (framework: SdkFramework) => void;
  inProgress: boolean;
  prompt: string | undefined;
  setPrompt: (prompt: string | undefined) => void;
  result: string | undefined;
  resultLang: SdkLanguage | undefined;
  outdated: boolean;
  agentify: (toolsetSlug: string, environmentSlug: string) => Promise<string>;
}>({
  lang: "python",
  setLang: () => {},
  framework: OPENAI_AGENTS_SDK,
  setFramework: () => {},
  inProgress: false,
  prompt: undefined,
  setPrompt: () => {},
  result: undefined,
  resultLang: undefined,
  outdated: false,
  agentify: () => Promise.resolve(""),
});

export const AgentifyProvider = ({
  children,
}: {
  children: React.ReactNode;
}) => {
  const project = useProject();
  const messages = useChatMessages();
  const telemetry = useTelemetry();

  const [lang, setLang] = useState<SdkLanguage>("python");
  const [framework, setFramework] = useState<SdkFramework>(FRAMEWORKS[lang][0]);
  const [prompt, setPrompt] = useState<string>();
  const [inProgress, setInProgress] = useState(false);

  const [result, setResult] = useState<string | undefined>();
  const [resultLang, setResultLang] = useState<SdkLanguage | undefined>();
  const [resultFramework, setResultFramework] = useState<
    SdkFramework | undefined
  >();
  const [resultPrompt, setResultPrompt] = useState<string | undefined>();
  const [resultNumMessages, setResultNumMessages] = useState<
    number | undefined
  >();

  const model = useMiniModel();

  useEffect(() => {
    if (!Object.keys(FRAMEWORKS[lang]).includes(framework)) {
      setFramework(FRAMEWORKS[lang][0]);
    }
  }, [lang]);

  const agentify = async (toolsetSlug: string, environmentSlug: string) => {
    telemetry.capture("agentify_event", {
      action: "agentify_started",
      num_messages: messages.length,
      prompt,
      lang,
      framework,
    });

    setInProgress(true);

    const exampleUrl = Object.keys(AGENT_EXAMPLES[lang]).includes(framework)
      ? AGENT_EXAMPLES[lang][
          framework as keyof (typeof AGENT_EXAMPLES)[typeof lang]
        ]
      : Object.values(AGENT_EXAMPLES[lang])[0];

    const example = await fetch(exampleUrl!).then((res) => res.text());

    const result = await generateObject({
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      model: model as any,
      mode: "json",
      prompt: `
<instructions>
  You will be given a chat history, a statement of intent, and a basic skeleton of an agent.
  Using the statement of intent and details from the chat history, produce a complete agent in ${lang} using ${framework} that performs the task.
  The agent should use LLM calls and toolsets to solve the generic version of the task as described in the statement of intent, not just the specific example given.
  Note that the toolset provides tools and handles their execution and authentication. For example, any tool call present in the chat history will be available to the agent.
  The agents structure should closely mirror the example agent, but should be tailored to the specific task and chat history.
</instructions>

<statement-of-intent>
  ${prompt}
</statement-of-intent>

<values-to-use>
  <project>
      ${project.slug}
  </project>
  <toolset>
      ${toolsetSlug}
  </toolset>
  <environment>
      ${environmentSlug}
  </environment>
</values-to-use>

<chat-history>
  ${messages.map((m) => `${m.role}: ${m.parts.map((p) => (p.type === "text" ? p.text : "")).join("")}`).join("\n\t")}
</chat-history>

<example-agent>
  ${example}
</example-agent>
      `,
      temperature: 0.5,
      schema: z.object({
        agentCode: z.string(),
      }),
    });

    setInProgress(false);
    setResult((result.object as { agentCode: string }).agentCode);
    setResultLang(lang);
    setResultFramework(framework);
    setResultPrompt(prompt);
    setResultNumMessages(messages.length);

    return (result.object as { agentCode: string }).agentCode;
  };

  const outdated =
    lang !== resultLang ||
    framework !== resultFramework ||
    prompt !== resultPrompt ||
    messages.length !== resultNumMessages;

  return (
    <AgentifyContext.Provider
      value={{
        lang,
        setLang,
        framework,
        setFramework,
        agentify,
        inProgress,
        prompt,
        setPrompt,
        result,
        resultLang,
        outdated,
      }}
    >
      {children}
    </AgentifyContext.Provider>
  );
};

export const AgentifyButton = ({
  toolsetSlug,
  environmentSlug,
  onAgentify,
}: {
  toolsetSlug: string;
  environmentSlug: string;
  onAgentify: () => void;
}) => {
  const session = useSession();
  const project = useProject();
  const telemetry = useTelemetry();
  const messages = useChatMessages();
  const { agentify, inProgress, prompt, setPrompt, lang, setLang } =
    useAgentify();

  const [suggestionNumMessages, setSuggestionNumMessages] = useState(0);
  const [agentifyModalOpen, setAgentifyModalOpen] = useState(false);

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

    setPrompt(undefined);

    telemetry.capture("agentify_event", {
      action: "agentify_modal_opened",
      num_messages: messages.length,
      prompt,
      lang,
    });

    generateObject({
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      model: openrouter.chat("openai/gpt-4o-mini") as any,
      mode: "json",
      prompt: `
          <instructions>
            You will be given a chat history.
            Your job is to distill the user's intent from the chat history to produce a few sentences that describe the function the agent should perform, based on the user's intent from the chat history.
            This prompt will be used to generate an agent that can reusably and extensibly solve the task.
            Phrase the prompt as instructions, e.g. "Find all new users from the last 30 days and send them a welcome email".
          </instructions>

          <chat-history>
            ${messages.map((m) => `${m.role}: ${m.parts.map((p) => (p.type === "text" ? p.text : "")).join("")}`).join("\n\t")}
          </chat-history>
          `,
      temperature: 0.5,
      schema: z.object({
        promptSuggestion: z.string(),
      }),
    }).then((result) => {
      setPrompt(
        (result.object as { promptSuggestion: string }).promptSuggestion,
      );
      setSuggestionNumMessages(messages.length);
    });
  }, [agentifyModalOpen]);

  const agentifyFn = async () => {
    agentify(toolsetSlug, environmentSlug);
    onAgentify();
    setAgentifyModalOpen(false);
  };

  const agentifyAvailable =
    messages.filter((m) => m.role === "user").length > 0;
  const agentifyButton = (
    <Button
      variant="secondary"
      size="sm"
      disabled={!agentifyAvailable}
      onClick={() => setAgentifyModalOpen(true)}
    >
      AGENTIFY
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
          <Stack gap={4}>
            <Stack gap={1}>
              <Heading variant="h5" className="normal-case font-medium">
                What language should the agent be written in?
              </Heading>
              <SdkLanguageDropdown lang={lang} setLang={setLang} />
            </Stack>
            <Stack gap={1}>
              <Heading variant="h5" className="normal-case font-medium">
                {prompt
                  ? "What should the agent do?"
                  : "Distilling chat history..."}
              </Heading>
              {prompt ? (
                <>
                  <TextArea
                    value={prompt}
                    onChange={(value) => setPrompt(value)}
                    disabled={inProgress}
                    placeholder="What should the agent do?"
                    rows={4}
                  />
                  <Type muted variant="small" italic>
                    The chat history will also be used to generate the agent
                    code.
                  </Type>
                </>
              ) : (
                <SkeletonParagraph lines={4} />
              )}
            </Stack>
          </Stack>
          <Dialog.Footer>
            <Button
              variant="tertiary"
              onClick={() => setAgentifyModalOpen(false)}
            >
              Back
            </Button>
            <Button onClick={agentifyFn} disabled={!prompt || inProgress}>
              {inProgress && <Spinner />}
              {inProgress ? "Generating..." : "Agentify"}
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </>
  );
};
