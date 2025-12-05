import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { useState } from "react";

interface ChatInputProps {
  onSend: (message: string) => void;
  disabled?: boolean;
  placeholder?: string;
  className?: string;
  initialInput?: string;
  additionalActions?: React.ReactNode;
}

export function ChatInput({
  onSend,
  disabled,
  placeholder = "Type a message...",
  className,
  initialInput,
  additionalActions,
}: ChatInputProps) {
  const [input, setInput] = useState(initialInput || "");

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (input.trim() && !disabled) {
      onSend(input);
      setInput("");
    }
  };

  return (
    <form
      onSubmit={handleSubmit}
      className={cn("flex items-center gap-2 border-t p-4", className)}
    >
      <textarea
        value={input}
        onChange={(e) => setInput(e.target.value)}
        placeholder={placeholder}
        disabled={disabled}
        className="flex-1 resize-none rounded-lg border bg-background px-3 py-2 text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50"
        rows={1}
        onKeyDown={(e) => {
          if (e.key === "Enter" && !e.shiftKey) {
            e.preventDefault();
            handleSubmit(e);
          }
        }}
      />
      {additionalActions}
      <Button type="submit" disabled={disabled || !input.trim()}>
        Send
      </Button>
    </form>
  );
}
