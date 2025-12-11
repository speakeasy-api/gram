import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { ToolExecutionLog } from "@gram/client/models/components";
import { Badge, Button, Icon } from "@speakeasy-api/moonshine";
import { useState } from "react";

interface LogExecutionDetailSheetProps {
  log: ToolExecutionLog | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

function formatTimestamp(date: Date): string {
  return new Intl.DateTimeFormat("en-US", {
    year: "numeric",
    month: "long",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
    timeZoneName: "short",
  }).format(date);
}

function getLevelBadgeVariant(level: string): "neutral" | "information" | "warning" | "destructive" {
  switch (level.toLowerCase()) {
    case "debug":
      return "neutral";
    case "info":
      return "information";
    case "warn":
    case "warning":
      return "warning";
    case "error":
      return "destructive";
    default:
      return "neutral";
  }
}

function copyToClipboard(text: string) {
  navigator.clipboard.writeText(text);
}

function AttributesViewer({ jsonString }: { jsonString: string | undefined }) {
  if (!jsonString) {
    return (
      <div className="text-muted-foreground text-sm italic">
        No attributes available
      </div>
    );
  }

  try {
    const parsed = JSON.parse(jsonString);
    const entries = Object.entries(parsed);

    if (entries.length === 0) {
      return (
        <div className="text-muted-foreground text-sm italic">
          No attributes available
        </div>
      );
    }

    const formatValue = (value: unknown): string => {
      if (value === null) return "null";
      if (value === undefined) return "undefined";
      if (typeof value === "object") return JSON.stringify(value, null, 2);
      return String(value);
    };

    return (
      <div className="space-y-3">
        {entries.map(([key, value]) => (
          <div
            key={key}
            className="flex justify-between items-start py-2 px-3 border-b border-neutral-softest"
          >
            <span className="text-sm text-muted-foreground font-medium min-w-[120px]">
              {key.replace(/_/g, " ").replace(/\b\w/g, (l) => l.toUpperCase())}
            </span>
            <span className="text-sm font-mono text-right break-all flex-1">
              {formatValue(value)}
            </span>
          </div>
        ))}
      </div>
    );
  } catch {
    return (
      <div className="bg-surface-secondary-default p-4 rounded-md">
        <p className="text-destructive-default text-sm font-medium mb-2">
          Invalid JSON
        </p>
        <pre className="text-xs font-mono overflow-x-auto">{jsonString}</pre>
      </div>
    );
  }
}

export function LogExecutionDetailSheet({
  log,
  open,
  onOpenChange,
}: LogExecutionDetailSheetProps) {
  const [activeTab, setActiveTab] = useState<"details" | "attributes">("details");

  if (!log) return null;

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="w-[600px] sm:max-w-[600px] overflow-y-auto">
        <SheetHeader>
          <SheetTitle>Log Details</SheetTitle>
        </SheetHeader>

        <div className="flex gap-2 mt-4 mb-6 border-b border-neutral-softest">
          <button
            onClick={() => setActiveTab("details")}
            className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${
              activeTab === "details"
                ? "border-primary-default text-primary-default"
                : "border-transparent text-muted-foreground hover:text-foreground"
            }`}
          >
            Details
          </button>
          <button
            onClick={() => setActiveTab("attributes")}
            className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${
              activeTab === "attributes"
                ? "border-primary-default text-primary-default"
                : "border-transparent text-muted-foreground hover:text-foreground"
            }`}
          >
            Attributes
          </button>
        </div>

        {activeTab === "details" ? (
          <div className="space-y-6">
            {/* Message Section */}
            <div>
              <h3 className="text-sm font-semibold mb-2 px-3">Message</h3>
              <div className="bg-surface-secondary-default p-4 rounded-md">
                <p className="text-sm font-mono whitespace-pre-wrap break-words">
                  {log.message || log.rawLog}
                </p>
              </div>
            </div>

            {/* Raw Log Section (if different from message) */}
            {log.message && log.message !== log.rawLog && (
              <div>
                <h3 className="text-sm font-semibold mb-2 px-3">Raw Log</h3>
                <div className="bg-surface-secondary-default p-4 rounded-md relative">
                  <Button
                    size="sm"
                    variant="secondary"
                    onClick={() => copyToClipboard(log.rawLog)}
                    className="absolute top-2 right-2"
                  >
                    <Button.LeftIcon>
                      <Icon name="copy" className="size-3" />
                    </Button.LeftIcon>
                    <Button.Text>Copy</Button.Text>
                  </Button>
                  <pre className="text-xs font-mono whitespace-pre-wrap break-words pr-20">
                    {log.rawLog}
                  </pre>
                </div>
              </div>
            )}

            {/* Properties Section */}
            <div>
              <h3 className="text-sm font-semibold mb-3 px-3">Properties</h3>
              <div className="space-y-3">
                <div className="flex justify-between items-center py-2 px-3 border-b border-neutral-softest">
                  <span className="text-sm text-muted-foreground">Timestamp</span>
                  <span className="text-sm font-mono">
                    {formatTimestamp(log.timestamp)}
                  </span>
                </div>

                <div className="flex justify-between items-center py-2 px-3 border-b border-neutral-softest">
                  <span className="text-sm text-muted-foreground">Level</span>
                  <Badge
                    variant={getLevelBadgeVariant(log.level)}
                    className="font-mono text-xs"
                  >
                    {log.level.toUpperCase()}
                  </Badge>
                </div>

                <div className="flex justify-between items-center py-2 px-3 border-b border-neutral-softest">
                  <span className="text-sm text-muted-foreground">Source</span>
                  <Badge
                    variant={log.source === "stderr" ? "warning" : "neutral"}
                    className="font-mono text-xs"
                  >
                    {log.source}
                  </Badge>
                </div>

                <div className="flex justify-between items-center py-2 px-3 border-b border-neutral-softest">
                  <span className="text-sm text-muted-foreground">Instance</span>
                  <span className="text-sm font-mono">{log.instance}</span>
                </div>

                <div className="flex justify-between items-center py-2 px-3 border-b border-neutral-softest">
                  <span className="text-sm text-muted-foreground">Function ID</span>
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-mono">{log.functionId}</span>
                    <Button
                      size="sm"
                      variant="secondary"
                      onClick={() => copyToClipboard(log.functionId)}
                    >
                      <Button.LeftIcon>
                        <Icon name="copy" className="size-3" />
                      </Button.LeftIcon>
                    </Button>
                  </div>
                </div>

                <div className="flex justify-between items-center py-2 px-3 border-b border-neutral-softest">
                  <span className="text-sm text-muted-foreground">
                    Deployment ID
                  </span>
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-mono">{log.deploymentId}</span>
                    <Button
                      size="sm"
                      variant="secondary"
                      onClick={() => copyToClipboard(log.deploymentId)}
                    >
                      <Button.LeftIcon>
                        <Icon name="copy" className="size-3" />
                      </Button.LeftIcon>
                    </Button>
                  </div>
                </div>

                <div className="flex justify-between items-center py-2 px-3 border-b border-neutral-softest">
                  <span className="text-sm text-muted-foreground">Project ID</span>
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-mono">{log.projectId}</span>
                    <Button
                      size="sm"
                      variant="secondary"
                      onClick={() => copyToClipboard(log.projectId)}
                    >
                      <Button.LeftIcon>
                        <Icon name="copy" className="size-3" />
                      </Button.LeftIcon>
                    </Button>
                  </div>
                </div>

                <div className="flex justify-between items-center py-2 px-3 border-b border-neutral-softest">
                  <span className="text-sm text-muted-foreground">Log ID</span>
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-mono">{log.id}</span>
                    <Button
                      size="sm"
                      variant="secondary"
                      onClick={() => copyToClipboard(log.id)}
                    >
                      <Button.LeftIcon>
                        <Icon name="copy" className="size-3" />
                      </Button.LeftIcon>
                    </Button>
                  </div>
                </div>
              </div>
            </div>
          </div>
        ) : (
          <div>
            <h3 className="text-sm font-semibold mb-3 px-3">Log Attributes</h3>
            <AttributesViewer jsonString={log.attributes} />
          </div>
        )}
      </SheetContent>
    </Sheet>
  );
}
