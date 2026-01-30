import { ChatSummary } from "@gram/client/models/components";
import {
  MessageSquareIcon,
  WrenchIcon,
  ClockIcon,
  UserIcon,
  CoinsIcon,
} from "lucide-react";
import { StatusBadge } from "./StatusBadge";
import { formatDuration, formatNanoTimestamp, formatTokenCount } from "./utils";

interface ConversationRowProps {
  chat: ChatSummary;
  isSelected: boolean;
  onSelect: () => void;
}

export function ConversationRow({
  chat,
  isSelected,
  onSelect,
}: ConversationRowProps) {
  return (
    <div
      className={`flex flex-col gap-2 px-4 py-3 cursor-pointer border-b border-border/50 last:border-b-0 transition-colors ${
        isSelected
          ? "border-l-2 border-l-primary-default bg-primary-softest"
          : "border-l-2 border-l-transparent hover:bg-surface-secondary-default"
      }`}
      onClick={onSelect}
    >
      {/* Top row: ID + status */}
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-2 min-w-0">
          <span className="text-xs font-mono text-muted-foreground truncate">
            {chat.gramChatId.slice(0, 12)}
          </span>
          <StatusBadge status={chat.status} />
        </div>
        <span className="text-xs text-muted-foreground shrink-0">
          {formatNanoTimestamp(chat.startTimeUnixNano)}
        </span>
      </div>

      {/* Stats row */}
      <div className="flex items-center gap-4 text-xs text-muted-foreground">
        {chat.userId && (
          <span className="flex items-center gap-1 truncate max-w-[120px]">
            <UserIcon className="size-3 shrink-0" />
            {chat.userId}
          </span>
        )}
        <span className="flex items-center gap-1">
          <ClockIcon className="size-3 shrink-0" />
          {formatDuration(chat.durationSeconds)}
        </span>
        <span className="flex items-center gap-1">
          <MessageSquareIcon className="size-3 shrink-0" />
          {chat.messageCount}
        </span>
        <span className="flex items-center gap-1">
          <WrenchIcon className="size-3 shrink-0" />
          {chat.toolCallCount}
        </span>
        {chat.totalTokens > 0 && (
          <span className="flex items-center gap-1">
            <CoinsIcon className="size-3 shrink-0" />
            {formatTokenCount(chat.totalTokens)}
          </span>
        )}
      </div>

      {/* Model badge */}
      {chat.model && (
        <div>
          <span className="px-1.5 py-0.5 text-[10px] font-medium rounded bg-surface-secondary-default text-muted-foreground">
            {chat.model}
          </span>
        </div>
      )}
    </div>
  );
}
