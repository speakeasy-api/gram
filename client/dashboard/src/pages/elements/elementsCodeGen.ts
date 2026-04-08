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

export function getDangerousApiKeyEnvContent(
  params: Pick<CodeGenParams, "apiKey">,
) {
  const apiKey = params.apiKey || "your_api_key_here";
  return `GRAM_API_KEY=${apiKey}`;
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

function buildConfigLines(params: CodeGenParams): string[] {
  const { projectSlug, mcpUrl, config } = params;
  const configLines: string[] = [];
  configLines.push(`  projectSlug: "${projectSlug}",`);
  configLines.push(`  mcp: "${mcpUrl}",`);

  if (config.variant !== "standalone") {
    configLines.push(`  variant: "${config.variant}",`);
  }
  if (config.colorScheme !== "system") {
    configLines.push(`  colorScheme: "${config.colorScheme}",`);
  }
  if (config.density !== "normal") {
    configLines.push(`  density: "${config.density}",`);
  }
  if (config.radius !== "soft") {
    configLines.push(`  radius: "${config.radius}",`);
  }

  const welcomeParts: string[] = [];
  if (config.welcomeTitle && config.welcomeTitle !== "Welcome") {
    welcomeParts.push(`    title: "${config.welcomeTitle}",`);
  }
  if (
    config.welcomeSubtitle &&
    config.welcomeSubtitle !== "How can I help you today?"
  ) {
    welcomeParts.push(`    subtitle: "${config.welcomeSubtitle}",`);
  }
  if (welcomeParts.length > 0) {
    configLines.push(`  welcome: {\n${welcomeParts.join("\n")}\n  },`);
  }

  if (
    config.composerPlaceholder &&
    config.composerPlaceholder !== "Send a message..."
  ) {
    configLines.push(
      `  composer: {\n    placeholder: "${config.composerPlaceholder}",\n  },`,
    );
  }
  if (config.showModelPicker) {
    configLines.push(`  model: {\n    showModelPicker: true,\n  },`);
  }
  if (config.systemPrompt) {
    const escapedPrompt = config.systemPrompt
      .replace(/\\/g, "\\\\")
      .replace(/"/g, '\\"')
      .replace(/\n/g, "\\n");
    configLines.push(`  systemPrompt: "${escapedPrompt}",`);
  }
  if (config.variant === "widget") {
    const modalParts: string[] = [];
    if (config.modalTitle && config.modalTitle !== "Chat") {
      modalParts.push(`    title: "${config.modalTitle}",`);
    }
    if (config.modalPosition !== "bottom-right") {
      modalParts.push(`    position: "${config.modalPosition}",`);
    }
    if (config.modalDefaultOpen) {
      modalParts.push(`    defaultOpen: true,`);
    }
    if (modalParts.length > 0) {
      configLines.push(`  modal: {\n${modalParts.join("\n")}\n  },`);
    }
  }
  if (config.expandToolGroupsByDefault) {
    configLines.push(`  tools: {\n    expandToolGroupsByDefault: true,\n  },`);
  }

  return configLines;
}

export function getSessionComponentCode(params: CodeGenParams): string {
  const { framework, projectSlug } = params;
  const isNextjs = framework === "nextjs";
  const useClientDirective = isNextjs ? `"use client";\n\n` : "";
  const sessionEndpoint = isNextjs
    ? "/api/session"
    : "http://localhost:3001/chat/session";

  const configLines = buildConfigLines(params);
  configLines.push(`  api: {\n    session: getSession,\n  },`);

  return `${useClientDirective}import { Chat, ElementsConfig, GramElementsProvider } from "@gram-ai/elements";

const getSession = async () => {
  return fetch("${sessionEndpoint}", {
    method: "POST",
    headers: { "Gram-Project": "${projectSlug}" },
  })
    .then((res) => res.json())
    .then((data) => data.client_token);
};

const config: ElementsConfig = {
${configLines.join("\n")}
};

export default function GramChat() {
  return (
    <GramElementsProvider config={config}>
      <Chat />
    </GramElementsProvider>
  );
}`;
}

export function getDangerousApiKeyComponentCode(params: CodeGenParams): string {
  const { framework } = params;
  const isNextjs = framework === "nextjs";
  const useClientDirective = isNextjs ? `"use client";\n\n` : "";

  const configLines = buildConfigLines(params);
  configLines.push(
    `  api: {\n    dangerousApiKey: process.env.GRAM_API_KEY!,\n  },`,
  );

  return `${useClientDirective}import { Chat, ElementsConfig, GramElementsProvider } from "@gram-ai/elements";

const config: ElementsConfig = {
${configLines.join("\n")}
};

export default function GramChat() {
  return (
    <GramElementsProvider config={config}>
      <Chat />
    </GramElementsProvider>
  );
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
