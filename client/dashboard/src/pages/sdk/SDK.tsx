import { Page } from "@/components/page-layout";
import { useState } from "react";
import { CodeSnippet } from "@speakeasy-api/moonshine";

const VERCEL_AI_SDK = "Vercel AI SDK" as const;
const LANGCHAIN = "LangChain" as const;
const OPENAI_AGENTS_SDK = "OpenAI Agents SDK" as const;
const FUNCION_CALLING = "Funcion Calling" as const;

const FRAMEWORKS = {
  typescript: [VERCEL_AI_SDK, LANGCHAIN, FUNCION_CALLING] as const,
  python: [OPENAI_AGENTS_SDK, LANGCHAIN, FUNCION_CALLING] as const,
} as const;

type Language = keyof typeof FRAMEWORKS;
type Framework = (typeof FRAMEWORKS)[keyof typeof FRAMEWORKS][number];

const CODE_SAMPLES = {
  typescript: {
    [VERCEL_AI_SDK]: `import { generateText } from 'ai';
import { VercelAdapter } from "@gram/sdk/vercel";
import { createOpenAI } from "@ai-sdk/openai";

const key = "<GRAM_API_KEY>";
const vercelAdapter = new VercelAdapter(key);

const openai = createOpenAI({
    apiKey: process.env.OPENAI_API_KEY
});

const tools = await vercelAdapter.tools({
    project: "default",
    toolset: "my-toolset",
    environment: "default"
});

const result = await generateText({
    model: openai('gpt-4'),
    tools,
    maxSteps: 5,
    prompt: 'Can you tell me about my tools?'
});

console.log(result.text);`,
    [LANGCHAIN]: `import { LangchainAdapter } from "@gram/sdk/langchain";
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
  project: "default",
  toolset: "my-toolset",
  environment: "default",
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
    [FUNCION_CALLING]: `import { FunctionCallingAdapter } from "@gram/sdk/functioncalling";

const key = process.env.GRAM_API_KEY ?? "";

// vanilla client that matches the function calling interface for direct use with model provider APIs
const functionCallingAdapter = new FunctionCallingAdapter(key);

const tools = await functionCallingAdapter.tools({
  project: "default",
  toolset: "my-toolset",
  environment: "default",
});

// exposes name, description, parameters, and an execute and aexcute (async) function
console.log(tools[0].name)
console.log(tools[0].description)
console.log(tools[0].parameters)
console.log(tools[0].execute)`,
  },
  python: {
    [OPENAI_AGENTS_SDK]: `import asyncio
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
        project="default",
        toolset="my-toolset",
        environment="default",
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
    [LANGCHAIN]: `import asyncio
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
    project="default",
    toolset="my-toolset",
    environment="default",
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
    [FUNCION_CALLING]: `import os
from gram_ai.functioncalling import GramFunctionCalling

key = "<GRAM_API_KEY>"

# vanilla client that matches the function calling interface for direct use with model provider APIs
gram = GramFunctionCalling(api_key=key)

tools = gram.tools(
    project="default",
    toolset="my-toolset",
    environment="default",
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
  const [language, setLanguage] = useState<Language>("python");
  const [framework, setFramework] = useState<Framework>(OPENAI_AGENTS_SDK);

  const getCodeSample = () => {
    return CODE_SAMPLES[language][
      framework as keyof (typeof CODE_SAMPLES)[typeof language]
    ];
  };

  const handleLanguageChange = (newLanguage: Language) => {
    setLanguage(newLanguage);
    // If the current framework exists in the new language, keep it
    if (FRAMEWORKS[newLanguage].some((f) => f === framework)) {
      return;
    }

    setFramework(FRAMEWORKS[newLanguage][0]);
  };

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <div className="flex justify-between items-center mb-2">
          <h2>
            Use Gram toolsets to build agentic workflows in many popular
            frameworks
          </h2>

          <div className="flex gap-2">
            <select
              className="px-4 py-2 rounded border"
              value={language}
              onChange={(e) => handleLanguageChange(e.target.value as Language)}
            >
              {Object.keys(FRAMEWORKS).map((lang) => (
                <option key={lang} value={lang}>
                  {lang}
                </option>
              ))}
            </select>

            <select
              className="px-4 py-2 rounded border min-w-[200px]"
              value={framework}
              onChange={(e) => setFramework(e.target.value as Framework)}
            >
              {FRAMEWORKS[language].map((fw) => (
                <option key={fw} value={fw}>
                  {fw}
                </option>
              ))}
            </select>
          </div>
        </div>

        <div className="rounded border">
          <CodeSnippet
            code={getCodeSample()}
            language={language}
            copyable
            fontSize="medium"
            showLineNumbers
          />
        </div>
      </Page.Body>
    </Page>
  );
}
