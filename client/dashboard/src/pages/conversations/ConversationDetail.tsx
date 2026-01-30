import { ChatSummary } from "@gram/client/models/components";
import {
  ClockIcon,
  MessageSquareIcon,
  WrenchIcon,
  UserIcon,
  CpuIcon,
  CoinsIcon,
} from "lucide-react";
import { StatusBadge } from "./StatusBadge";
import { ConversationLogsList } from "./ConversationLogsList";
import {
  formatDuration,
  formatNanoTimestamp,
  formatTokenCount,
  nanoToDate,
} from "./utils";
import { dateTimeFormatters } from "@/lib/dates";

interface ConversationDetailProps {
  chat: ChatSummary;
}

function StatItem({
  icon: IconComponent,
  label,
  value,
}: {
  icon: React.ComponentType<{ className?: string }>;
  label: string;
  value: string | number;
}) {
  return (
    <div className="flex flex-col gap-1">
      <span className="text-[10px] uppercase tracking-wide text-muted-foreground font-medium">
        {label}
      </span>
      <span className="flex items-center gap-1.5 text-sm font-medium">
        <IconComponent className="size-3.5 text-muted-foreground" />
        {value}
      </span>
    </div>
  );
}

export function ConversationDetail({ chat }: ConversationDetailProps) {
  const startDate = nanoToDate(chat.startTimeUnixNano);

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="flex flex-col gap-4 px-6 py-5 border-b border-border">
        <div className="flex items-start justify-between">
          <div className="flex flex-col gap-1">
            <span className="text-sm font-mono text-foreground">
              {chat.gramChatId}
            </span>
            <span className="text-xs text-muted-foreground">
              {dateTimeFormatters.full.format(startDate)}
            </span>
          </div>
          <StatusBadge status={chat.status} />
        </div>

        {/* Stats grid */}
        <div className="grid grid-cols-3 gap-4">
          {chat.userId && (
            <StatItem icon={UserIcon} label="User" value={chat.userId} />
          )}
          <StatItem
            icon={ClockIcon}
            label="Duration"
            value={formatDuration(chat.durationSeconds)}
          />
          <StatItem
            icon={MessageSquareIcon}
            label="Messages"
            value={chat.messageCount}
          />
          <StatItem
            icon={WrenchIcon}
            label="Tool Calls"
            value={chat.toolCallCount}
          />
          {chat.model && (
            <StatItem icon={CpuIcon} label="Model" value={chat.model} />
          )}
          {chat.totalTokens > 0 && (
            <StatItem
              icon={CoinsIcon}
              label="Tokens"
              value={`${formatTokenCount(chat.totalInputTokens)} in / ${formatTokenCount(chat.totalOutputTokens)} out`}
            />
          )}
        </div>
      </div>

      {/* Logs */}
      <div className="flex-1 overflow-y-auto">
        <div className="px-6 py-3 border-b border-border">
          <span className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
            Logs ({chat.logCount})
          </span>
        </div>
        <ConversationLogsList chatId={chat.gramChatId} />
      </div>
    </div>
  );
}

export function ConversationDetailEmpty() {
  return (
    <div className="flex items-center justify-center h-full text-muted-foreground">
      <div className="flex flex-col items-center gap-2">
        <MessageSquareIcon className="size-8 opacity-30" />
        <span className="text-sm">Select a conversation to view details</span>
      </div>
    </div>
  );
}
