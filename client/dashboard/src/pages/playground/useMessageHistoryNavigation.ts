import { useCallback, useEffect, useRef, useState } from "react";
import { UIMessage } from "ai";

export function useMessageHistoryNavigation(messages: UIMessage[]) {
  const [historyIndex, setHistoryIndex] = useState(-1);
  const [originalInputValue, setOriginalInputValue] = useState("");
  const inputRef = useRef<HTMLTextAreaElement | null>(null);

  const userMessages = messages
    .filter((m) => m.role === "user")
    .map((m) => m.parts.map((p) => (p.type === "text" ? p.text : "")).join(""))
    .reverse();

  const navigateHistory = useCallback(
    (direction: "up" | "down") => {
      if (userMessages.length === 0) {
        return;
      }

      let input = inputRef.current;
      if (!input) {
        const textarea = document.querySelector("textarea");
        if (textarea instanceof HTMLTextAreaElement) {
          inputRef.current = textarea;
          input = textarea;
        } else {
          return;
        }
      }

      if (!input) {
        return;
      }

      let newMessage = "";
      let newIndex = historyIndex;

      if (direction === "up") {
        if (historyIndex === -1) {
          setOriginalInputValue(input.value);
          newIndex = 0;
          newMessage = userMessages[0] || "";
        } else if (historyIndex < userMessages.length - 1) {
          newIndex = historyIndex + 1;
          newMessage = userMessages[newIndex] || "";
        } else {
          return;
        }
      } else {
        if (historyIndex > 0) {
          newIndex = historyIndex - 1;
          newMessage = userMessages[newIndex] || "";
        } else if (historyIndex === 0) {
          newIndex = -1;
          newMessage = originalInputValue;
        } else {
          return;
        }
      }

      setHistoryIndex(newIndex);
      input.value = newMessage;

      const inputEvent = new Event("input", { bubbles: true });
      const changeEvent = new Event("change", { bubbles: true });

      input.dispatchEvent(inputEvent);
      input.dispatchEvent(changeEvent);
      input.focus();

      // React synthetic event handling
      if ("_valueTracker" in input) {
        const descriptor =
          Object.getOwnPropertyDescriptor(input, "value") ||
          Object.getOwnPropertyDescriptor(
            HTMLTextAreaElement.prototype,
            "value",
          );
        if (descriptor && descriptor.set) {
          descriptor.set.call(input, newMessage);
          const tracker = (
            input as HTMLTextAreaElement & {
              _valueTracker?: { setValue: (value: string) => void };
            }
          )._valueTracker;
          if (tracker) {
            tracker.setValue("");
          }
          input.dispatchEvent(new Event("input", { bubbles: true }));
        }
      }
    },
    [historyIndex, userMessages, originalInputValue],
  );

  const handleKeyDown = useCallback(
    (event: KeyboardEvent) => {
      const target = event.target as HTMLElement;

      if (target.tagName !== "TEXTAREA") {
        return;
      }

      if (target instanceof HTMLTextAreaElement && !inputRef.current) {
        inputRef.current = target;
      }

      if (event.key === "ArrowUp") {
        event.preventDefault();
        navigateHistory("up");
      } else if (event.key === "ArrowDown") {
        event.preventDefault();
        navigateHistory("down");
      } else if (event.key === "Escape") {
        setHistoryIndex(-1);
        setOriginalInputValue("");
      }
    },
    [navigateHistory],
  );

  useEffect(() => {
    document.addEventListener("keydown", handleKeyDown);
    return () => {
      document.removeEventListener("keydown", handleKeyDown);
    };
  }, [handleKeyDown]);

  useEffect(() => {
    const findChatInput = () => {
      const selectors = [
        '[data-slot="composer"] textarea',
        ".ai-chat-container textarea",
        'textarea[placeholder*="message"]',
        'textarea[placeholder*="Message"]',
        'textarea[aria-label*="message"]',
        'textarea[aria-label*="Message"]',
        '[role="textbox"]',
        ".playground textarea",
        "textarea",
      ];

      for (const selector of selectors) {
        const textarea = document.querySelector(selector);
        if (textarea instanceof HTMLTextAreaElement) {
          inputRef.current = textarea;
          return true;
        }
      }
      return false;
    };

    if (!findChatInput()) {
      const observer = new MutationObserver(() => {
        findChatInput();
      });
      observer.observe(document.body, { childList: true, subtree: true });

      const timeout1 = setTimeout(() => {
        findChatInput();
      }, 1000);

      const timeout2 = setTimeout(() => {
        findChatInput();
      }, 3000);

      return () => {
        observer.disconnect();
        clearTimeout(timeout1);
        clearTimeout(timeout2);
      };
    }

    return undefined;
  }, []);

  useEffect(() => {
    setHistoryIndex(-1);
    setOriginalInputValue("");
  }, [messages.length]);

  return {
    historyIndex,
    userMessages,
    isNavigating: historyIndex >= 0,
    currentMessage: historyIndex >= 0 ? userMessages[historyIndex] : null,
    totalMessages: userMessages.length,
  };
}
