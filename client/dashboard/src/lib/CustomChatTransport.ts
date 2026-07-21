import {
  streamText,
  convertToModelMessages,
  UIMessage,
  smoothStream,
  toUIMessageStream,
  type ChatTransport,
  type LanguageModel,
  type ToolSet,
  type UIMessageChunk,
} from "ai";

export interface CustomChatTransportConfig {
  model: LanguageModel;
  temperature: number;
  maxGeneratedTokens?: number;
  getTools: (messages: UIMessage[]) => Promise<{
    tools: ToolSet;
    systemPrompt: string;
  }>;
  onError?: (error: { error: unknown }) => void;
}

export class CustomChatTransport implements ChatTransport<UIMessage> {
  private config: CustomChatTransportConfig;

  constructor(config: CustomChatTransportConfig) {
    this.config = config;
  }

  async sendMessages({
    messages,
  }: {
    messages: UIMessage[];
  }): Promise<ReadableStream<UIMessageChunk>> {
    // Get tools and system prompt dynamically per request
    const { tools, systemPrompt } = await this.config.getTools(messages);

    console.log(
      "CustomChatTransport: Sending request with tools:",
      Object.keys(tools),
    );
    console.log("CustomChatTransport: Tool count:", Object.keys(tools).length);

    const result = streamText({
      model: this.config.model,
      messages: await convertToModelMessages(messages),
      tools,
      temperature: this.config.temperature,
      maxOutputTokens: this.config.maxGeneratedTokens,
      instructions: systemPrompt,
      experimental_transform: smoothStream({ delayInMs: 15 }),
      onError: this.config.onError,
    });

    return toUIMessageStream({ stream: result.stream, tools });
  }

  reconnectToStream(): Promise<ReadableStream<UIMessageChunk> | null> {
    // Custom transport does not support stream reconnection — return null
    // to signal that there is no active stream to resume.
    return Promise.resolve(null);
  }

  updateConfig(config: Partial<CustomChatTransportConfig>): void {
    this.config = { ...this.config, ...config };
  }
}
