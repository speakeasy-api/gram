import {
  FUNCTION_CALLING,
  LANGCHAIN,
  LANGGRAPH,
  OPENAI_AGENTS_SDK,
  VERCEL_AI_SDK,
} from "./SDK";

export const AGENT_EXAMPLES = {
  python: {
    [OPENAI_AGENTS_SDK]:
      "https://raw.githubusercontent.com/speakeasy-api/gram-examples/refs/heads/main/python/agents_sdk/gtm_agent.py",
    [LANGCHAIN]:
      "https://raw.githubusercontent.com/speakeasy-api/gram-examples/refs/heads/main/python/langchain/gtm_agent.py",
  },
  typescript: {
    [VERCEL_AI_SDK]:
      "https://raw.githubusercontent.com/speakeasy-api/gram-examples/refs/heads/main/typescript/vercel/gtmAgent.ts",
    [LANGCHAIN]:
      "https://raw.githubusercontent.com/speakeasy-api/gram-examples/refs/heads/main/typescript/langchain/gtmAgent.ts",
    [LANGGRAPH]:
      "https://raw.githubusercontent.com/speakeasy-api/gram-examples/refs/heads/main/typescript/langgraph/gtmAgent.ts",
    [FUNCTION_CALLING]:
      "https://raw.githubusercontent.com/speakeasy-api/gram-examples/refs/heads/main/typescript/function_calling/gtmAgent.ts",
  },
};

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
const vercelAdapter = new VercelAdapter({apiKey: key});

const openai = createOpenAI({
    apiKey: process.env.OPENAI_API_KEY
});

const tools = await vercelAdapter.tools({
    project: "${project}",
    toolset: "${toolset}",
    environment: "${environment}",
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
const langchainAdapter = new LangchainAdapter({apiKey: key});

const llm = new ChatOpenAI({
  modelName: "gpt-4",
  temperature: 0,
  openAIApiKey: process.env.OPENAI_API_KEY,
});

const tools = await langchainAdapter.tools({
  project: "${project}",
  toolset: "${toolset}",
  environment: "${environment}",
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
const functionCallingAdapter = new FunctionCallingAdapter({apiKey: key});

const tools = await functionCallingAdapter.tools({
  project: "${project}",
  toolset: "${toolset}",
  environment: "${environment}",
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
        project="${project}",
        toolset="${toolset}",
        environment="${environment}",
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
    project="${project}",
    toolset="${toolset}",
    environment="${environment}",
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
    project="${project}",
    toolset="${toolset}",
    environment="${environment}",
)

# exposes name, description, parameters, and an execute and aexecute (async) function
print(tools[0].name)
print(tools[0].description)
print(tools[0].parameters)
print(tools[0].execute)
print(tools[0].aexecute)`,
  },
} as const;
