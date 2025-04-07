import type { Message } from "ai";

export type AIChatMessage = Message;

export interface AIChatPromptProps {
  onSend: (message: string) => void;
  disabled?: boolean;
  onStop?: () => void;
  onRegenerate?: () => void;
  onSuggestionClick?: (suggestion: string) => void;
}

export interface AIChatSuggestion {
  text: string;
  emoji: string;
  fullPrompt: string;
}
