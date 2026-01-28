import { Metrics } from "@gram/client/models/components";
import { formatNumber } from "./charts/utils";
import {
  MessageSquareIcon,
  CoinsIcon,
  ClockIcon,
  CheckCircleIcon,
  type LucideIcon,
} from "lucide-react";
import { cn } from "@/lib/utils";

interface MetricsCardsProps {
  metrics: Metrics;
}

function formatDuration(ms: number): string {
  if (ms < 1000) {
    return `${Math.round(ms)}ms`;
  }
  return `${(ms / 1000).toFixed(2)}s`;
}

export function MetricsCards({ metrics }: MetricsCardsProps) {
  const cards: Array<{
    title: string;
    value: string;
    subtitle: string;
    icon: LucideIcon;
    iconBg: string;
    iconColor: string;
  }> = [
    {
      title: "Total Chats",
      value: formatNumber(metrics.totalChats),
      subtitle: `${formatNumber(metrics.totalChatRequests)} requests`,
      icon: MessageSquareIcon,
      iconBg: "bg-chart-1/10",
      iconColor: "text-chart-1",
    },
    {
      title: "Total Tokens",
      value: formatNumber(metrics.totalTokens),
      subtitle: `${formatNumber(Math.round(metrics.avgTokensPerRequest))} avg/request`,
      icon: CoinsIcon,
      iconBg: "bg-chart-2/10",
      iconColor: "text-chart-2",
    },
    {
      title: "Avg Tool Duration",
      value: formatDuration(metrics.avgToolDurationMs),
      subtitle: "per tool call",
      icon: ClockIcon,
      iconBg: "bg-chart-4/10",
      iconColor: "text-chart-4",
    },
    {
      title: "Tool Success Rate",
      value: (() => {
        const total = metrics.toolCallSuccess + metrics.toolCallFailure;
        if (total === 0) return "N/A";
        return `${((metrics.toolCallSuccess / total) * 100).toFixed(1)}%`;
      })(),
      subtitle: `${formatNumber(metrics.toolCallSuccess)} of ${formatNumber(metrics.toolCallSuccess + metrics.toolCallFailure)} calls`,
      icon: CheckCircleIcon,
      iconBg: "bg-chart-5/10",
      iconColor: "text-chart-5",
    },
  ];

  return (
    <div className="grid grid-cols-4 gap-3">
      {cards.map((card) => (
        <div
          key={card.title}
          className="p-4 rounded-xl border border-border bg-card hover:border-primary/20 transition-colors"
        >
          <div className="flex flex-col gap-3">
            <div className="flex items-center justify-between">
              <div
                className={cn(
                  "flex items-center justify-center size-9 rounded-lg",
                  card.iconBg
                )}
              >
                <card.icon className={cn("size-5", card.iconColor)} />
              </div>
            </div>
            <div className="flex flex-col gap-0.5">
              <span className="text-xs text-muted-foreground font-medium">
                {card.title}
              </span>
              <span className="text-2xl font-bold tracking-tight">
                {card.value}
              </span>
              <span className="text-xs text-muted-foreground">
                {card.subtitle}
              </span>
            </div>
          </div>
        </div>
      ))}
    </div>
  );
}
