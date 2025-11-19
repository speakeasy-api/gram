import { createOpenAI } from "@ai-sdk/openai";
import { convertToModelMessages, streamText } from "ai";
import type { IncomingMessage, ServerResponse } from "node:http";

const openai = createOpenAI({
  apiKey: process.env.OPENAI_API_KEY,
});

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
