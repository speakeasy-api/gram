import { useEffect, useRef, useState } from "react";
import { cn } from "@/lib/utils";
import { Icon } from "@speakeasy-api/moonshine";
import { Type } from "@/components/ui/type";

export interface Tool {
  id: string;
  name: string;
  description?: string;
  type: "http" | "prompt";
  httpMethod?: string;
  path?: string;
}

interface ToolMentionProps {
  tools: Tool[];
  onToolSelected: (toolIds: string[]) => void;
  inputValue: string;
  onInputChange: (value: string) => void;
  textareaRef?: React.RefObject<HTMLTextAreaElement>;
}

export function parseMentionedTools(text: string, tools: Tool[]): string[] {
  // Find all @toolName mentions in the text
  const mentionPattern = /@(\w+)/g;
  const mentions: string[] = [];
  let match;

  while ((match = mentionPattern.exec(text)) !== null) {
    mentions.push(match[1].toLowerCase());
  }

  // Find tools that match the mentions
  const matchedToolIds = tools
    .filter((tool) => mentions.includes(tool.name.toLowerCase()))
    .map((tool) => tool.id);

  return [...new Set(matchedToolIds)]; // Remove duplicates
}

export function ToolMentionAutocomplete({
  tools,
  onToolSelected,
  inputValue,
  onInputChange,
  textareaRef,
}: ToolMentionProps) {
  const [showSuggestions, setShowSuggestions] = useState(false);
  const [filteredTools, setFilteredTools] = useState<Tool[]>([]);
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [cursorPosition, setCursorPosition] = useState(0);
  const suggestionsRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    // Parse the input to find @ mentions being typed
    const textarea = textareaRef?.current;
    if (!textarea) {
      return;
    }

    const position = textarea.selectionStart;
    setCursorPosition(position);

    // Find if we're currently typing an @ mention
    const textBeforeCursor = inputValue.slice(0, position);
    const lastAtSymbol = textBeforeCursor.lastIndexOf("@");

    if (lastAtSymbol !== -1) {
      const textAfterAt = textBeforeCursor.slice(lastAtSymbol + 1);

      // Check if we're still in the middle of typing a mention (no space after @)
      if (!textAfterAt.includes(" ") && !textAfterAt.includes("\n")) {
        const query = textAfterAt.toLowerCase();

        // Filter tools based on the query
        const filtered = tools.filter((tool) => {
          const nameMatch = tool.name.toLowerCase().includes(query);
          const descMatch = tool.description?.toLowerCase().includes(query);
          return nameMatch || descMatch;
        });

        setFilteredTools(filtered);
        setShowSuggestions(filtered.length > 0);
        setSelectedIndex(0);
      } else {
        setShowSuggestions(false);
      }
    } else {
      setShowSuggestions(false);
    }

    // Also parse completed mentions to notify parent
    const mentionedToolIds = parseMentionedTools(inputValue, tools);
    onToolSelected(mentionedToolIds);
  }, [inputValue, tools, onToolSelected, textareaRef]);

  const insertToolMention = (tool: Tool) => {
    const textarea = textareaRef?.current;
    if (!textarea) return;

    const textBeforeCursor = inputValue.slice(0, cursorPosition);
    const lastAtSymbol = textBeforeCursor.lastIndexOf("@");

    if (lastAtSymbol !== -1) {
      const beforeMention = inputValue.slice(0, lastAtSymbol);
      const afterCursor = inputValue.slice(cursorPosition);
      const newText = `${beforeMention}@${tool.name} ${afterCursor}`;

      onInputChange(newText);
      setShowSuggestions(false);

      // Set cursor position after the inserted mention
      setTimeout(() => {
        const newPosition = lastAtSymbol + tool.name.length + 2; // +2 for @ and space
        textarea.setSelectionRange(newPosition, newPosition);
        textarea.focus();
      }, 0);
    }
  };

  const handleKeyDown = (e: KeyboardEvent) => {
    if (!showSuggestions) return;

    switch (e.key) {
      case "ArrowDown":
        e.preventDefault();
        e.stopPropagation();
        setSelectedIndex((prev) => (prev + 1) % filteredTools.length);
        break;
      case "ArrowUp":
        e.preventDefault();
        e.stopPropagation();
        setSelectedIndex(
          (prev) => (prev - 1 + filteredTools.length) % filteredTools.length,
        );
        break;
      case "Enter":
      case "Tab":
        e.preventDefault();
        e.stopPropagation();
        if (filteredTools[selectedIndex]) {
          insertToolMention(filteredTools[selectedIndex]);
        }
        break;
      case "Escape":
        e.preventDefault();
        e.stopPropagation();
        setShowSuggestions(false);
        break;
    }
  };

  useEffect(() => {
    const textarea = textareaRef?.current;
    if (!textarea) return;

    textarea.addEventListener("keydown", handleKeyDown);
    return () => textarea.removeEventListener("keydown", handleKeyDown);
  }, [showSuggestions, filteredTools, selectedIndex, textareaRef]);

  // Get position for suggestions dropdown
  const getSuggestionsPosition = () => {
    const textarea = textareaRef?.current;
    if (!textarea) return { bottom: 0, left: 0 };

    // Position the dropdown above the textarea
    const rect = textarea.getBoundingClientRect();
    const containerRect = textarea
      .closest(".relative")
      ?.getBoundingClientRect();

    if (!containerRect) {
      return { bottom: 0, left: 0 };
    }

    return {
      bottom: window.innerHeight - rect.top + 8,
      left: rect.left - containerRect.left,
      maxWidth: rect.width,
    };
  };

  if (!showSuggestions) {
    return null;
  }

  const position = getSuggestionsPosition();

  return (
    <div
      ref={suggestionsRef}
      className={cn(
        "absolute z-[100] min-w-[200px] max-w-[400px] rounded-md border bg-popover shadow-md",
        "max-h-[200px] overflow-auto",
      )}
      style={{
        bottom: `${position.bottom}px`,
        left: `${position.left}px`,
        maxWidth: `${position.maxWidth}px`,
      }}
    >
      <div className="p-1">
        {filteredTools.map((tool, index) => (
          <button
            type="button"
            key={tool.id}
            className={cn(
              "flex w-full items-start gap-2 rounded px-2 py-1.5 text-left text-sm transition-colors",
              "hover:bg-accent hover:text-accent-foreground",
              index === selectedIndex && "bg-accent text-accent-foreground",
            )}
            onClick={(e) => {
              e.preventDefault();
              e.stopPropagation();
              insertToolMention(tool);
            }}
            onMouseEnter={() => setSelectedIndex(index)}
          >
            <Icon
              name={tool.type === "http" ? "globe" : "message-square"}
              className="mt-0.5 h-3 w-3 flex-shrink-0 opacity-50"
            />
            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-1">
                <Type variant="small" className="font-medium">
                  {tool.name}
                </Type>
                {tool.httpMethod && tool.path && (
                  <Type variant="small" className="text-muted-foreground">
                    {tool.httpMethod} {tool.path}
                  </Type>
                )}
              </div>
              {tool.description && (
                <Type
                  variant="small"
                  className="text-muted-foreground line-clamp-2"
                >
                  {tool.description}
                </Type>
              )}
            </div>
          </button>
        ))}
      </div>
    </div>
  );
}

// Helper function to display mentioned tools as badges
export function MentionedToolsBadges({
  toolIds,
  tools,
  onRemove,
}: {
  toolIds: string[];
  tools: Tool[];
  onRemove?: (toolId: string) => void;
}) {
  const mentionedTools = tools.filter((tool) => toolIds.includes(tool.id));

  if (mentionedTools.length === 0) return null;

  return (
    <div className="flex flex-wrap gap-1 p-2 border-t bg-background">
      <Type variant="small" className="text-muted-foreground mr-1">
        Selected tools:
      </Type>
      {mentionedTools.map((tool) => (
        <div
          key={tool.id}
          className={cn(
            "inline-flex items-center gap-1 rounded-md px-2 py-0.5",
            "bg-primary/10 text-primary text-xs",
          )}
        >
          <Icon
            name={tool.type === "http" ? "globe" : "message-square"}
            className="h-3 w-3"
          />
          <span>{tool.name}</span>
          {onRemove && (
            <button
              onClick={() => onRemove(tool.id)}
              className="ml-1 hover:opacity-70"
            >
              <Icon name="x" className="h-3 w-3" />
            </button>
          )}
        </div>
      ))}
    </div>
  );
}
