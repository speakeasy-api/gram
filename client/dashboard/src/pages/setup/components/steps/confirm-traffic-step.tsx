import { useState, useEffect } from "react";
import {
  Activity,
  Check,
  Loader2,
  TrendingUp,
  TrendingDown,
  Minus,
  PartyPopper,
} from "lucide-react";
import { StepContainer } from "../step-container";
import { MOCK_TRAFFIC_METRICS } from "../../mock-data";
import type { TrafficMetric } from "../../types";
import { cn } from "@/lib/utils";

interface ConfirmTrafficStepProps {
  onComplete: () => void;
  onBack: () => void;
}

export function ConfirmTrafficStep({
  onComplete,
  onBack,
}: ConfirmTrafficStepProps) {
  const [checking, setChecking] = useState(true);
  const [metrics, setMetrics] = useState<TrafficMetric[]>([]);
  const [allHealthy, setAllHealthy] = useState(false);

  useEffect(() => {
    const timer = setTimeout(() => {
      setMetrics(MOCK_TRAFFIC_METRICS);
      setChecking(false);
      setAllHealthy(MOCK_TRAFFIC_METRICS.every((m) => m.healthy));
    }, 2500);
    return () => clearTimeout(timer);
  }, []);

  const TrendIcon = ({ trend }: { trend: TrafficMetric["trend"] }) => {
    if (trend === "up") return <TrendingUp className="text-success h-4 w-4" />;
    if (trend === "down")
      return <TrendingDown className="text-destructive h-4 w-4" />;
    return <Minus className="text-muted-foreground h-4 w-4" />;
  };

  if (checking) {
    return (
      <StepContainer
        icon={
          <div className="bg-secondary flex h-12 w-12 items-center justify-center rounded-lg">
            <Activity className="text-foreground h-6 w-6" />
          </div>
        }
        title="Verifying traffic"
        description="Checking that everything is connected and working properly..."
        onContinue={() => {}}
        showBack
        onBack={onBack}
        canContinue={false}
        isLoading
      >
        <div className="flex flex-col items-center justify-center py-16">
          <div className="relative mb-6">
            <div className="bg-foreground/10 absolute inset-0 animate-ping rounded-full" />
            <div className="bg-secondary relative flex h-16 w-16 items-center justify-center rounded-full">
              <Loader2 className="text-foreground h-8 w-8 animate-spin" />
            </div>
          </div>
          <p className="text-muted-foreground text-sm">
            Verifying connectivity and compliance
          </p>
        </div>
      </StepContainer>
    );
  }

  return (
    <StepContainer
      icon={
        <div className="bg-secondary flex h-12 w-12 items-center justify-center rounded-lg">
          <Activity className="text-foreground h-6 w-6" />
        </div>
      }
      title="Confirm traffic"
      description="Verify that traffic is flowing and your team is compliant with configured policies."
      onContinue={onComplete}
      continueLabel="Go to Dashboard"
      showBack
      onBack={onBack}
    >
      <div className="space-y-6">
        {/* Health status banner */}
        <div
          className={cn(
            "rounded-lg border p-6",
            allHealthy
              ? "border-success/20 bg-success/5"
              : "border-destructive/20 bg-destructive/5",
          )}
        >
          <div className="flex items-center gap-4">
            <div
              className={cn(
                "flex h-14 w-14 items-center justify-center rounded-full",
                allHealthy ? "bg-success" : "bg-destructive",
              )}
            >
              <Check className="text-background h-7 w-7" />
            </div>
            <div>
              <p className="text-foreground text-xl font-semibold">
                {allHealthy ? "All systems healthy" : "Attention required"}
              </p>
              <p className="text-muted-foreground">
                {allHealthy
                  ? "Traffic is flowing and compliance is verified"
                  : "Some metrics require your attention"}
              </p>
            </div>
          </div>
        </div>

        {/* Metrics grid */}
        <div className="grid grid-cols-2 gap-4">
          {metrics.map((metric, index) => (
            <div
              key={index}
              className={cn(
                "rounded-lg border p-4",
                metric.healthy
                  ? "border-border bg-card"
                  : "border-destructive/20 bg-destructive/5",
              )}
            >
              <div className="mb-2 flex items-center justify-between">
                <span className="text-muted-foreground text-sm">
                  {metric.label}
                </span>
                <TrendIcon trend={metric.trend} />
              </div>
              <p className="text-foreground text-2xl font-semibold">
                {metric.value}
              </p>
            </div>
          ))}
        </div>

        {/* Live activity */}
        <div className="border-border bg-card overflow-hidden rounded-lg border">
          <div className="border-border flex items-center justify-between border-b px-4 py-3">
            <span className="text-foreground text-sm font-medium">
              Recent activity
            </span>
            <span className="text-success flex items-center gap-1.5 text-xs">
              <span className="bg-success h-1.5 w-1.5 animate-pulse rounded-full" />
              Live
            </span>
          </div>
          <div className="space-y-3 p-4">
            {[
              {
                user: "sarah.chen@acme.com",
                action: "Accessed GitHub MCP",
                time: "2s ago",
                status: "allowed",
              },
              {
                user: "marcus.j@acme.com",
                action: "Tool call: read_file",
                time: "5s ago",
                status: "allowed",
              },
              {
                user: "e.rodriguez@acme.com",
                action: "Requested: npm_install",
                time: "12s ago",
                status: "pending",
              },
              {
                user: "d.kim@acme.com",
                action: "Accessed Slack MCP",
                time: "18s ago",
                status: "allowed",
              },
            ].map((event, i) => (
              <div key={i} className="flex items-center gap-3 text-sm">
                <span
                  className={cn(
                    "h-2 w-2 flex-shrink-0 rounded-full",
                    event.status === "allowed" && "bg-success",
                    event.status === "blocked" && "bg-destructive",
                    event.status === "pending" && "bg-chart-4",
                  )}
                />
                <span className="text-foreground flex-1 truncate">
                  <span className="font-medium">{event.user}</span>
                  <span className="text-muted-foreground">
                    {" "}
                    - {event.action}
                  </span>
                </span>
                <span className="text-muted-foreground flex-shrink-0 text-xs">
                  {event.time}
                </span>
              </div>
            ))}
          </div>
        </div>

        {/* Success message */}
        {allHealthy && (
          <div className="bg-foreground/5 border-foreground/10 rounded-lg border p-4">
            <div className="flex items-start gap-3">
              <div className="bg-foreground mt-0.5 flex h-8 w-8 flex-shrink-0 items-center justify-center rounded">
                <PartyPopper className="text-background h-4 w-4" />
              </div>
              <div>
                <p className="text-foreground text-sm font-medium">
                  Setup complete!
                </p>
                <p className="text-muted-foreground mt-1 text-sm">
                  Your organization is ready to use Speakeasy.
                </p>
              </div>
            </div>
          </div>
        )}
      </div>
    </StepContainer>
  );
}
