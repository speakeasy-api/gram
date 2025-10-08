import { useEffect, useRef, useState } from "react";
import {
  ToolMentionAutocomplete,
  MentionedToolsBadges,
  Tool,
} from "./ToolMentions";

interface ChatComposerWrapperProps {
  children: React.ReactNode;
  tools: Tool[];
  onToolsSelected: (toolIds: string[]) => void;
  onInputChange?: (value: string) => void;
}

export function ChatComposerWrapper({
  children,
  tools,
  onToolsSelected,
  onInputChange,
}: ChatComposerWrapperProps) {
  const [inputValue, setInputValue] = useState("");
  const [mentionedToolIds, setMentionedToolIds] = useState<string[]>([]);
  const [badgesBottomOffset, setBadgesBottomOffset] = useState(0);
  const containerRef = useRef<HTMLDivElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null!);
  const lastValueRef = useRef("");

  // Find and attach to the textarea in the chat composer
  useEffect(() => {
    if (!containerRef.current) return;

    const findTextarea = () => {
      const textarea = containerRef.current?.querySelector("textarea");
      if (textarea && textarea !== textareaRef.current) {
        textareaRef.current = textarea;
      }
      return textarea;
    };

    // Try to find textarea immediately
    findTextarea();

    // Also observe for changes in case the textarea is added later
    const observer = new MutationObserver(() => {
      findTextarea();
    });

    observer.observe(containerRef.current, {
      childList: true,
      subtree: true,
    });

    return () => {
      observer.disconnect();
    };
  }, []);

  // Separate effect for polling that doesn't depend on inputValue
  useEffect(() => {
    const pollInterval = setInterval(() => {
      const textarea = textareaRef.current;
      if (textarea) {
        const value = textarea.value;
        if (value !== lastValueRef.current) {
          lastValueRef.current = value;
          setInputValue(value);
          onInputChange?.(value);
        }

        // Update badges position based on the chat input container height
        const chatContainer =
          textarea.closest('[class*="composer"]') || textarea.parentElement;
        if (chatContainer && containerRef.current) {
          const containerRect = containerRef.current.getBoundingClientRect();
          const chatContainerRect = chatContainer.getBoundingClientRect();
          const offset = chatContainerRect.bottom - containerRect.top + 8;
          setBadgesBottomOffset(offset);
        }
      }
    }, 100);

    return () => {
      clearInterval(pollInterval);
    };
  }, [onInputChange]);

  // Update parent when tools are selected
  useEffect(() => {
    onToolsSelected(mentionedToolIds);
  }, [mentionedToolIds, onToolsSelected]);

  const handleInputChange = (value: string) => {
    lastValueRef.current = value;
    setInputValue(value);
    if (textareaRef.current) {
      // Use the native setter to trigger React's change detection
      const nativeInputValueSetter = Object.getOwnPropertyDescriptor(
        window.HTMLTextAreaElement.prototype,
        "value",
      )?.set;

      if (nativeInputValueSetter) {
        nativeInputValueSetter.call(textareaRef.current, value);
      } else {
        textareaRef.current.value = value;
      }

      // Only dispatch input event to update the controlled component
      // Don't dispatch change or other events that might trigger submission
      const inputEvent = new Event("input", { bubbles: true });
      textareaRef.current.dispatchEvent(inputEvent);
    }
  };

  return (
    <div ref={containerRef} className="relative h-full w-full">
      {children}
      {textareaRef.current && (
        <ToolMentionAutocomplete
          tools={tools}
          onToolSelected={setMentionedToolIds}
          inputValue={inputValue}
          onInputChange={handleInputChange}
          textareaRef={textareaRef}
        />
      )}
      {mentionedToolIds.length > 0 &&
        textareaRef.current &&
        badgesBottomOffset > 0 && (
          <div
            className="absolute left-0 right-0 z-50 pointer-events-none"
            style={{ bottom: `${badgesBottomOffset}px` }}
          >
            <div className="pointer-events-auto">
              <MentionedToolsBadges
                toolIds={mentionedToolIds}
                tools={tools}
                onRemove={(toolId) => {
                  setMentionedToolIds((prev) =>
                    prev.filter((id) => id !== toolId),
                  );
                  // Remove the mention from input text
                  const tool = tools.find((t) => t.id === toolId);
                  if (tool) {
                    handleInputChange(
                      inputValue.replace(
                        new RegExp(`@${tool.name}\\s*`, "g"),
                        "",
                      ),
                    );
                  }
                }}
              />
            </div>
          </div>
        )}
    </div>
  );
}
