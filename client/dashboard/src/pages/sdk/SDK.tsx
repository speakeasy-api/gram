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
  prompt: 'Write a propmpt using tools.',
});

console.log(result.output);`,
    'LangChain': `// COMING SOON - LangChain TypeScript Sample`
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
        "Write a propmpt using tools.",
    )
    print(result.final_output)


if __name__ == "__main__":
    asyncio.run(main())`,
    'LangChain': `# COMING SOON - LangChain Python Sample`
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
