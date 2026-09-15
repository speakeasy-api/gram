// Spike harness only: points Pi at the scripted local model so the run needs
// no real provider credentials.
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

export default function scriptedProvider(pi: ExtensionAPI) {
  pi.registerProvider("scripted", {
    baseUrl: process.env.SCRIPTED_MODEL_URL ?? "http://127.0.0.1:8932/v1",
    apiKey: "not-a-secret",
    api: "openai-completions",
    models: [
      {
        id: "scripted-1",
        name: "Scripted",
        reasoning: false,
        input: ["text"],
        cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
        contextWindow: 128000,
        maxTokens: 4096,
      },
    ],
  });
}
