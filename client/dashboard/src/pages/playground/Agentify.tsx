import { useProject } from "@/contexts/Auth";
import { useTelemetry } from "@/contexts/Telemetry";
import { generateObject } from "ai";
import { useEffect, useState } from "react";
import { z } from "zod";
import {
  AGENT_EXAMPLES,
  FRAMEWORKS,
  SdkFramework,
  SdkLanguage,
} from "../sdk/examples";
import { useChatMessages } from "./useChatContext";
import { useMiniModel } from "./Openrouter";
import { AgentifyContext } from "./useAgentify";

export const AgentifyProvider = ({
  children,
}: {
  children: React.ReactNode;
}): JSX.Element => {
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
  }, [lang, framework]);

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
      model,
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
