import { useCallback, useMemo, useRef, useState } from "react";
import { useAui, useAuiState } from "@assistant-ui/react";
import {
  MentionableTool,
  toolSetToMentionableTools,
} from "@/elements/lib/tool-mentions";

export interface UseToolMentionsOptions {
  tools: Record<string, unknown> | undefined;
  enabled?: boolean;
}

export interface UseToolMentionsReturn {
  mentionableTools: MentionableTool[];
  value: string;
  cursorPosition: number;
  textareaRef: React.RefObject<HTMLTextAreaElement | null>;
  updateCursorPosition: () => void;
  handleAutocompleteChange: (value: string, cursorPosition: number) => void;
  isActive: boolean;
}

export function useToolMentions({
  tools,
  enabled = true,
}: UseToolMentionsOptions): UseToolMentionsReturn {
  const [cursorPosition, setCursorPosition] = useState(0);
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);
  const aui = useAui();
  const composerText = useAuiState(({ composer }) => composer.text);

  const mentionableTools = useMemo(
    () => toolSetToMentionableTools(tools),
    [tools],
  );

  const updateCursorPosition = useCallback(() => {
    const textarea = textareaRef.current;
    if (textarea) {
      setCursorPosition(textarea.selectionStart);
    }
  }, []);

  const handleAutocompleteChange = useCallback(
    (newValue: string, newCursorPosition: number) => {
      aui.composer().setText(newValue);
      setCursorPosition(newCursorPosition);

      setTimeout(() => {
        const textarea = textareaRef.current;
        if (textarea) {
          textarea.focus();
          textarea.setSelectionRange(newCursorPosition, newCursorPosition);
        }
      }, 0);
    },
    [aui],
  );

  const isActive = enabled && mentionableTools.length > 0;

  return {
    mentionableTools,
    value: composerText,
    cursorPosition,
    textareaRef,
    updateCursorPosition,
    handleAutocompleteChange,
    isActive,
  };
}
