import { generateText } from 'ai';
import { VercelAdapter } from '../src/vercel';
import { createOpenAI } from "@ai-sdk/openai";

const key = process.env.GRAM_API_KEY ?? "";
const vercelAdapter = new VercelAdapter(key);

const openai = createOpenAI({
    apiKey: process.env.GRAM_OPENAI_API_KEY
});

const tools = await vercelAdapter.tools({
    project: "default",
    toolset: "test",
    environment: "default"
});

const result = await generateText({
    model: openai('gpt-4'),
    tools,
    maxSteps: 5,
    prompt: 'Get me the speakeasy organization ryan-local.'
});

console.log(result.text);