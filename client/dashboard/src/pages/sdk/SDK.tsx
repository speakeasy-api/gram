import { Page } from "@/components/page-layout";
import { Combobox } from "@/components/ui/combobox";
import { Type } from "@/components/ui/type";
import { useProject } from "@/contexts/Auth";
import { capitalize } from "@/lib/utils";
import { CodeSnippet } from "@speakeasy-api/moonshine";
import { useState } from "react";

const VERCEL_AI_SDK = "Vercel AI SDK" as const;
const LANGCHAIN = "LangChain" as const;
const OPENAI_AGENTS_SDK = "OpenAI Agents SDK" as const;
const FUNCTION_CALLING = "Function Calling" as const;

const FRAMEWORKS = {
  typescript: [VERCEL_AI_SDK, LANGCHAIN, FUNCTION_CALLING] as const,
  python: [OPENAI_AGENTS_SDK, LANGCHAIN, FUNCTION_CALLING] as const,
} as const;

type Language = keyof typeof FRAMEWORKS;
type Framework = (typeof FRAMEWORKS)[keyof typeof FRAMEWORKS][number];

export const CODE_SAMPLES = {
  typescript: {
    [VERCEL_AI_SDK]: (
      project: string,
      toolset: string,
      environment: string
    ) => `import { generateText } from 'ai';
import { VercelAdapter } from "@gram-ai/sdk/vercel";
import { createOpenAI } from "@ai-sdk/openai";

const key = "<GRAM_API_KEY>";
const vercelAdapter = new VercelAdapter(key);

const openai = createOpenAI({
    apiKey: process.env.OPENAI_API_KEY
});

const tools = await vercelAdapter.tools({
    project: ${project},
    toolset: ${toolset},
    environment: ${environment}
});

const result = await generateText({
    model: openai('gpt-4'),
    tools,
    maxSteps: 5,
    prompt: 'Can you tell me about my tools?'
});

console.log(result.text);`,
    [LANGCHAIN]: (
      project: string,
      toolset: string,
      environment: string
    ) => `import { LangchainAdapter } from "@gram-ai/sdk/langchain";
import { ChatOpenAI } from "@langchain/openai";
import { createOpenAIFunctionsAgent, AgentExecutor } from "langchain/agents";
import { pull } from "langchain/hub";
import { ChatPromptTemplate } from "@langchain/core/prompts";

const key = "<GRAM_API_KEY>";
const langchainAdapter = new LangchainAdapter(key);

const llm = new ChatOpenAI({
  modelName: "gpt-4",
  temperature: 0,
  openAIApiKey: process.env.OPENAI_API_KEY,
});

const tools = await langchainAdapter.tools({
  project: ${project},
  toolset: ${toolset},
  environment: ${environment},
});

const prompt = await pull<ChatPromptTemplate>(
  "hwchase17/openai-functions-agent"
);

const agent = await createOpenAIFunctionsAgent({
  llm,
  tools,
  prompt
});

const executor = new AgentExecutor({
  agent,
  tools,
  verbose: false,
});

const result = await executor.invoke({
  input: "Can you tell me about my tools?",
});

console.log(result.output);`,
    [FUNCTION_CALLING]: (
      project: string,
      toolset: string,
      environment: string
    ) => `import { FunctionCallingAdapter } from "@gram-ai/sdk/functioncalling";

const key = process.env.GRAM_API_KEY ?? "";

// vanilla client that matches the function calling interface for direct use with model provider APIs
const functionCallingAdapter = new FunctionCallingAdapter(key);

const tools = await functionCallingAdapter.tools({
  project: ${project},
  toolset: ${toolset},
  environment: ${environment},
});

// exposes name, description, parameters, and an execute and aexcute (async) function
console.log(tools[0].name)
console.log(tools[0].description)
console.log(tools[0].parameters)
console.log(tools[0].execute)`,
  },
  python: {
    [OPENAI_AGENTS_SDK]: (
      project: string,
      toolset: string,
      environment: string
    ) => `import asyncio
import os
from agents import Agent, Runner, set_default_openai_key
from gram_ai.openai_agents import GramOpenAIAgents

gram = GramLangchain(api_key=key)

gram = GramOpenAIAgents(
    api_key=key,
)

set_default_openai_key(os.getenv("OPENAI_API_KEY"))

agent = Agent(
    name="Assistant",
    tools=gram.tools(
        project=${project},
        toolset=${toolset},
        environment=${environment},
    ),
)


async def main():
    result = await Runner.run(
        agent,
        "Can you tell me about my tools?",
    )
    print(result.final_output)


if __name__ == "__main__":
    asyncio.run(main())`,
    [LANGCHAIN]: (
      project: string,
      toolset: string,
      environment: string
    ) => `import asyncio
import os
from langchain import hub
from langchain_openai import ChatOpenAI
from langchain.agents import AgentExecutor, create_openai_functions_agent
from gram_ai.langchain import GramLangchain

key = "<GRAM_API_KEY>"

gram = GramLangchain(api_key=key)

llm = ChatOpenAI(
    model="gpt-4",
    temperature=0,
    openai_api_key=os.getenv("OPENAI_API_KEY")
)

tools = gram.tools(
    project=${project},
    toolset=${toolset},
    environment=${environment},
)

prompt = hub.pull("hwchase17/openai-functions-agent")

agent = create_openai_functions_agent(llm=llm, tools=tools, prompt=prompt)

agent_executor = AgentExecutor(agent=agent, tools=tools, verbose=False)

async def main():
    response = await agent_executor.ainvoke({
        "input": "Can you tell me about my tools?"
    })
    print(response)

if __name__ == "__main__":
    asyncio.run(main())`,
    [FUNCTION_CALLING]: (
      project: string,
      toolset: string,
      environment: string
    ) => `import os
from gram_ai.function_calling import GramFunctionCalling

key = "<GRAM_API_KEY>"

# vanilla client that matches the function calling interface for direct use with model provider APIs
gram = GramFunctionCalling(api_key=key)

tools = gram.tools(
    project=${project},
    toolset=${toolset},
    environment=${environment},
)

# exposes name, description, parameters, and an execute and aexecute (async) function
print(tools[0].name)
print(tools[0].description)
print(tools[0].parameters)
print(tools[0].execute)
print(tools[0].aexecute)`,
  },
} as const;

export default function SDK() {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <SdkContent />
      </Page.Body>
    </Page>
  );
}

export const SdkContent = ({
  projectSlug,
  toolset = "my-toolset",
  environment = "default",
  codeOverride,
  codeOverrideLanguage,
}: {
  projectSlug?: string;
  toolset?: string;
  environment?: string;
  codeOverride?: string;
  codeOverrideLanguage?: Language;
}) => {
  const project = useProject();

  const [lang, setLang] = useState<Language>("python");
  const [framework, setFramework] = useState<Framework>(OPENAI_AGENTS_SDK);

  const getCodeSample = () => {
    return (
      codeOverride ??
      CODE_SAMPLES[lang][
        framework as keyof (typeof CODE_SAMPLES)[typeof lang]
      ](projectSlug ?? project.slug, toolset, environment)
    );
  };

  const handleLanguageChange = (newLanguage: Language) => {
    setLang(newLanguage);
    // If the current framework exists in the new language, keep it
    if (FRAMEWORKS[newLanguage].some((f) => f === framework)) {
      return;
    }

    setFramework(FRAMEWORKS[newLanguage][0]);
  };

  const languageDropdownItems =
    Object.keys(FRAMEWORKS).map((lang) => ({
      label: capitalize(lang),
      value: lang,
    })) ?? [];

  const languageDropdown = (
    <Combobox
      items={languageDropdownItems}
      selected={lang}
      onSelectionChange={(value) =>
        handleLanguageChange(value.value as Language)
      }
      className="max-w-fit"
    >
      <Type variant="small" className="capitalize">
        {lang}
      </Type>
    </Combobox>
  );

  const frameworkDropdownItems =
    FRAMEWORKS[lang].map((fw) => ({
      label: fw,
      value: fw,
    })) ?? [];

  const frameworkDropdown = (
    <Combobox
      items={frameworkDropdownItems}
      selected={framework}
      onSelectionChange={(value) => setFramework(value.value as Framework)}
      className="max-w-fit"
    >
      <Type variant="small">{framework}</Type>
    </Combobox>
  );

  return (
    <div>
      <div className="flex justify-between items-center mb-2">
        <h2>
          Use Gram toolsets to build agentic workflows in many popular
          frameworks
        </h2>

        <div className="flex gap-2">
          {languageDropdown}
          {frameworkDropdown}
        </div>
      </div>

      <CodeSnippet
        code={getCodeSample()}
        language={codeOverrideLanguage ?? lang}
        copyable
        fontSize="medium"
        showLineNumbers
        className="border-border"
      />
    </div>
  );
};
