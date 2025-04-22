import { LangchainAdapter } from "../src/langchain";
import { ChatOpenAI } from "@langchain/openai";
import { createOpenAIFunctionsAgent, AgentExecutor } from "langchain/agents";
import { pull } from "langchain/hub";
import { ChatPromptTemplate } from "@langchain/core/prompts";

const key = process.env.GRAM_API_KEY ?? "";
const langchainAdapter = new LangchainAdapter(key);

const llm = new ChatOpenAI({
  modelName: "gpt-4",
  temperature: 0,
  openAIApiKey: process.env.GRAM_OPENAI_API_KEY,
});

const tools = await langchainAdapter.tools({
  project: "default",
  toolset: "test",
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
  input: "Can you get me the speakeasy organization ryan-local",
});

console.log(result.output);
