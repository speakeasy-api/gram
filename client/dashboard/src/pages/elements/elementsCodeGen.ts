import type { ElementsFormConfig } from "./Elements";

export interface CodeGenParams {
  apiKey: string | null;
  framework: "nextjs" | "react";
  projectSlug: string;
  mcpUrl: string;
  config: ElementsFormConfig;
}

export function getEnvContent(params: Pick<CodeGenParams, "apiKey">) {
  const apiKey = params.apiKey || "your_api_key_here";
  return `GRAM_API_KEY=${apiKey}
EMBED_ORIGIN=http://localhost:3000 # Replace with your actual origin`;
}

export function getPeerDeps(params: Pick<CodeGenParams, "framework">) {
  const pm = params.framework === "nextjs" ? "npm" : "pnpm";
  return `${pm} add react react-dom @assistant-ui/react @assistant-ui/react-markdown motion remark-gfm zustand vega shiki`;
}

export function getElementsInstall(params: Pick<CodeGenParams, "framework">) {
  const pm = params.framework === "nextjs" ? "npm" : "pnpm";
  return `${pm} add @gram-ai/elements`;
}

export function getNextjsApiRoute(): string {
  return `// pages/api/session.ts
import type { NextApiRequest, NextApiResponse } from "next";
import { createElementsServerHandlers } from "@gram-ai/elements/server";

// Disable Next.js body parsing so the handler can read the raw stream.
export const config = {
  api: {
    bodyParser: false,
  },
};

const handlers = createElementsServerHandlers();

export default async function handler(
  req: NextApiRequest,
  res: NextApiResponse
) {
  await handlers.session(req, res, {
    userIdentifier: "user-123", // Replace with actual user ID
    embedOrigin: process.env.EMBED_ORIGIN || "http://localhost:3000",
  });
}`;
}

export function getViteApiRoute(): string {
  return `// server.ts (Express)
import express from "express";
import { createElementsServerHandlers } from "@gram-ai/elements/server";

const app = express();
const handlers = createElementsServerHandlers();

app.use(express.json());

app.post("/chat/session", (req, res) =>
  handlers.session(req, res, {
    // Replace with your actual origin
    embedOrigin: process.env.EMBED_ORIGIN || "http://localhost:3000",
    userIdentifier: "user-123", // Replace with actual user ID
    expiresAfter: 3600,
  })
);

app.listen(3001, () => {
  console.log("Server running on http://localhost:3001");
});`;
}
