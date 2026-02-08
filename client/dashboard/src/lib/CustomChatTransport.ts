import {
  streamText,
  convertToModelMessages,
  UIMessage,
  smoothStream,
} from "ai";

export interface CustomChatTransportConfig {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  model: any;
  temperature: number;
  maxGeneratedTokens?: number;
  getTools: (messages: UIMessage[]) => Promise<{
    tools: Record<string, { description?: string; inputSchema: unknown }>;
    systemPrompt: string;
  }>;
  onError?: (error: { error: unknown }) => void;
}

export class CustomChatTransport {
  private config: CustomChatTransportConfig;

  constructor(config: CustomChatTransportConfig) {
    this.config = config;
  }

  async sendMessages({ messages }: { messages: UIMessage[] }) {
    // Get tools and system prompt dynamically per request
    const { tools, systemPrompt } = await this.config.getTools(messages);

    console.log(
      "CustomChatTransport: Sending request with tools:",
      Object.keys(tools),
    );
    console.log("CustomChatTransport: Tool count:", Object.keys(tools).length);

    const result = await streamText({
      model: this.config.model,
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      messages: convertToModelMessages(messages) as any,
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      tools: tools as any,
      temperature: this.config.temperature,
      maxOutputTokens: this.config.maxGeneratedTokens,
      system: systemPrompt,
      experimental_transform: smoothStream({ delayInMs: 15 }),
      providerOptions: {
        anthropic: {
          thinking: { type: "enabled", budgetTokens: 10000 },
        },
        openai: {
          reasoningSummary: "detailed",
        },
      },
      onError: this.config.onError,
    });

    return result.toUIMessageStream();
  }

  updateConfig(config: Partial<CustomChatTransportConfig>) {
    this.config = { ...this.config, ...config };
  }
}
