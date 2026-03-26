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
