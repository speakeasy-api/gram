"use client";

import { ThreadList } from "@/elements/components/assistant-ui/thread-list";
import { ShadowRoot } from "@/elements/components/ShadowRoot";

interface ChatHistoryProps {
  className?: string;
  /** Render the "New Thread" button above the list. Defaults to true. */
  showNewThread?: boolean;
}

export const ChatHistory = ({
  className,
  showNewThread,
}: ChatHistoryProps): React.JSX.Element => {
  return (
    <ShadowRoot hostStyle={{ height: "inherit", width: "inherit" }}>
      <ThreadList className={className} showNewThread={showNewThread} />
    </ShadowRoot>
  );
};
