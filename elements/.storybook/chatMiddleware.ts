import { createOpenAI } from "@ai-sdk/openai";
import { convertToModelMessages, streamText } from "ai";
import type { IncomingMessage, ServerResponse } from "node:http";

const openai = createOpenAI({
  apiKey: process.env.OPENAI_API_KEY,
});

/**
 * Storybook relies on vite's own dev server to run the storybook application.
 * We need to add a middleware to the storybook dev server to handle the chat API requests
 * so that we can use the chat API in the stories to test real LLM interactions.
 * @param req - The incoming request.
 * @param res - The outgoing response.
 * @param next - The next middleware function.
 * @returns
 */
export async function chatMiddleware(
  req: IncomingMessage,
  res: ServerResponse,
  next: () => void,
) {
  if (req.url === "/api/chat" && req.method === "POST") {
    try {
      const chunks: Buffer[] = [];
      for await (const chunk of req) {
        chunks.push(chunk);
      }
      const body = Buffer.concat(chunks).toString();
      const { messages } = JSON.parse(body);
      const result = streamText({
        model: openai.chat("gpt-4o-mini"),
        messages: convertToModelMessages(messages),
      });

      const response = result.toUIMessageStreamResponse();

      // Copy headers from the Response to ServerResponse
      response.headers.forEach((value, key) => {
        res.setHeader(key, value);
      });

      res.statusCode = response.status;

      // Pipe the response body
      if (response.body) {
        const reader = response.body.getReader();
        let chunkCount = 0;
        while (true) {
          const { done, value } = await reader.read();
          if (done) {
            break;
          }
          chunkCount++;
          res.write(value);
        }
      }
      res.end();
    } catch (error) {
      res.statusCode = 500;
      res.end(JSON.stringify({ error: String(error) }));
    }
  } else {
    next();
  }
}
