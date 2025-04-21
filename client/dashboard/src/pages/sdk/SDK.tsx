import { Page } from "@/components/page-layout";
import { useState } from "react";
import { CodeSnippet } from "@speakeasy-api/moonshine";

const FRAMEWORKS = {
  typescript: ['Vercel AI SDK', 'LangChain'] as const,
  python: ['OpenAI Agents SDK', 'LangChain'] as const
} as const;

type Language = keyof typeof FRAMEWORKS;
type Framework = typeof FRAMEWORKS[keyof typeof FRAMEWORKS][number];

const CODE_SAMPLES = {
  typescript: {
    'Vercel AI SDK': `import { generateText } from 'ai';
import { VercelAdapter } from '@gram/sdk/vercel';

const key = "<GRAM_API_KEY>"
const vercelAdapter = new VercelAdapter(key);

const tools = await vercelAdapter.tools({
    project="default",
    toolset="my-toolset",
    environment="local",
});

const result = await generateText({
  model: 'gpt-4',
  tools,
  prompt: 'Write a prompt using tools.',
});

console.log(result.output);`,
    'LangChain': `import { ChatOpenAI } from "@langchain/openai";
import { AgentExecutor, createToolCallingAgent } from "langchain/agents";
import { LangchainAdapter } from "@gram/sdk/langchain";

const key = "<GRAM_API_KEY>"
const langchainAdapter = new LangchainAdapter(key);

const tools = await langchainAdapter.tools({
  project: "default",
  toolset: "my-toolset",
  environment: "local",
});

const llm = new ChatOpenAI({
  modelName: "gpt-4-turbo",
  temperature: 0,
  apiKey: process.env.OPENAI_API_KEY,
});

const agent = await createToolCallingAgent({
  llm,
  tools,
});

const agentExecutor = new AgentExecutor({
  agent,
  tools,
});

const result = await agentExecutor.invoke({
  input: "Write a prompt using tools.",
});
console.log(result.output);`
  },
  python: {
    'OpenAI Agents SDK': `import asyncio
from agents import Agent, Runner
from gram_ai.openai_agents import GramOpenAIAgents

key = "<GRAM_API_KEY>"

gram = GramOpenAIAgents(
    api_key=key,
)

agent = Agent(
    name="Assistant",
    tools=gram.tools(
        project="default",
        toolset="my-toolset",
        environment="local",
    ),
)


async def main():
    result = await Runner.run(
        agent,
        "Write a prompt using tools.",
    )
    print(result.final_output)


if __name__ == "__main__":
    asyncio.run(main())`,
    'LangChain': `from langchain_openai import ChatOpenAI
from langchain.agents import create_tool_calling_agent, AgentExecutor
from gram_ai.langchain import GramLangchain

key = "<GRAM_API_KEY>"
adapter = GramLangchain(key)

tools = adapter.tools(
    project="default",
    toolset="my-toolset",
    environment="local",
)

llm = ChatOpenAI(
    model="gpt-4-turbo",
    temperature=0,
)

agent = create_tool_calling_agent(
    llm=llm,
    tools=tools,
)

executor = AgentExecutor(
    agent=agent,
    tools=tools,
)

result = executor.invoke({
    "input": "Write a prompt using tools.",
})

print(result["output"])`
  }
} as const;

export default function SDK() {
  const [language, setLanguage] = useState<Language>('typescript');
  const [framework, setFramework] = useState<Framework>('Vercel AI SDK');

  const getCodeSample = () => {
    return CODE_SAMPLES[language][framework as keyof (typeof CODE_SAMPLES)[typeof language]];
  };

  const handleLanguageChange = (newLanguage: Language) => {
    setLanguage(newLanguage);
    // If the current framework exists in the new language, keep it
    if (FRAMEWORKS[newLanguage].some(f => f === framework)) {
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
          <h2>Use Gram toolsets to build agentic workflows in many popular frameworks</h2>
          
          <div className="flex gap-2">
            <select 
              className="px-4 py-2 rounded border"
              value={language}
              onChange={(e) => handleLanguageChange(e.target.value as Language)}
            >
              {Object.keys(FRAMEWORKS).map(lang => (
                <option key={lang} value={lang}>{lang}</option>
              ))}
            </select>

            <select
              className="px-4 py-2 rounded border min-w-[200px]"
              value={framework}
              onChange={(e) => setFramework(e.target.value as Framework)}
            >
              {FRAMEWORKS[language].map(fw => (
                <option key={fw} value={fw}>{fw}</option>
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
