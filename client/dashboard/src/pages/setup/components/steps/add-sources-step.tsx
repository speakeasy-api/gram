import { useState } from "react";
import { Database, Search } from "lucide-react";
import { StepContainer } from "../step-container";
import { MCP_SOURCES } from "../../mock-data";
import type { McpSource } from "../../types";
import { Switch } from "@/components/ui/switch";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";

interface AddSourcesStepProps {
  onComplete: () => void;
  onBack: () => void;
}

export function AddSourcesStep({ onComplete, onBack }: AddSourcesStepProps) {
  const [sources, setSources] = useState<McpSource[]>(MCP_SOURCES);
  const [searchQuery, setSearchQuery] = useState("");
  const [activeTab, setActiveTab] = useState<"all" | "1st-party" | "3rd-party">(
    "all",
  );

  const toggleSource = (sourceId: string) => {
    setSources((prev) =>
      prev.map((s) => (s.id === sourceId ? { ...s, enabled: !s.enabled } : s)),
    );
  };

  const filteredSources = sources.filter((source) => {
    const matchesSearch = source.name
      .toLowerCase()
      .includes(searchQuery.toLowerCase());
    const matchesTab = activeTab === "all" || source.type === activeTab;
    return matchesSearch && matchesTab;
  });

  const enabledCount = sources.filter((s) => s.enabled).length;
  const firstPartyEnabled = sources.filter(
    (s) => s.type === "1st-party" && s.enabled,
  ).length;
  const thirdPartyEnabled = sources.filter(
    (s) => s.type === "3rd-party" && s.enabled,
  ).length;

  return (
    <StepContainer
      icon={
        <div className="bg-secondary flex h-12 w-12 items-center justify-center rounded-lg">
          <Database className="text-foreground h-6 w-6" />
        </div>
      }
      title="Add MCP sources"
      description="Configure which tools and data sources your agents can access. Sanctioned servers will be distributed to your team."
      onContinue={onComplete}
      continueLabel="Continue"
      showBack
      onBack={onBack}
    >
      <div className="space-y-6">
        {/* Stats */}
        <div className="grid grid-cols-3 gap-4">
          <div className="border-border bg-card rounded-lg border p-4">
            <p className="text-foreground text-2xl font-semibold">
              {enabledCount}
            </p>
            <p className="text-muted-foreground text-xs">Total enabled</p>
          </div>
          <div className="border-border bg-card rounded-lg border p-4">
            <p className="text-foreground text-2xl font-semibold">
              {firstPartyEnabled}
            </p>
            <p className="text-muted-foreground text-xs">1st party</p>
          </div>
          <div className="border-border bg-card rounded-lg border p-4">
            <p className="text-foreground text-2xl font-semibold">
              {thirdPartyEnabled}
            </p>
            <p className="text-muted-foreground text-xs">3rd party</p>
          </div>
        </div>

        {/* Search and filter */}
        <div className="flex items-center gap-3">
          <div className="relative flex-1">
            <Search className="text-muted-foreground absolute top-1/2 left-3 h-4 w-4 -translate-y-1/2" />
            <Input
              placeholder="Search sources..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="pl-9"
            />
          </div>
          <div className="border-border bg-card flex rounded-lg border p-1">
            {(["all", "1st-party", "3rd-party"] as const).map((tab) => (
              <button
                key={tab}
                onClick={() => setActiveTab(tab)}
                className={cn(
                  "rounded-md px-3 py-1.5 text-sm font-medium transition-colors",
                  activeTab === tab
                    ? "bg-secondary text-foreground"
                    : "text-muted-foreground hover:text-foreground",
                )}
              >
                {tab === "all"
                  ? "All"
                  : tab === "1st-party"
                    ? "1st Party"
                    : "3rd Party"}
              </button>
            ))}
          </div>
        </div>

        {/* Sources grid */}
        <div className="grid max-h-[280px] grid-cols-2 gap-2 overflow-auto">
          {filteredSources.map((source) => (
            <div
              key={source.id}
              className={cn(
                "flex items-center gap-3 rounded-lg border p-3 transition-colors",
                source.enabled
                  ? "border-foreground/20 bg-secondary/50"
                  : "border-border bg-card",
              )}
            >
              <div
                className={cn(
                  "flex h-9 w-9 flex-shrink-0 items-center justify-center rounded-lg",
                  source.enabled ? "bg-foreground/10" : "bg-secondary",
                )}
              >
                <span className="text-foreground text-sm font-semibold">
                  {source.name.charAt(0)}
                </span>
              </div>
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <p className="text-foreground truncate text-sm font-medium">
                    {source.name}
                  </p>
                  <span
                    className={cn(
                      "flex-shrink-0 rounded px-1.5 py-0.5 text-[10px] font-medium uppercase",
                      source.type === "1st-party"
                        ? "bg-foreground/10 text-foreground"
                        : "bg-secondary text-muted-foreground",
                    )}
                  >
                    {source.type === "1st-party" ? "1st" : "3rd"}
                  </span>
                </div>
                <p className="text-muted-foreground truncate text-xs">
                  {source.description}
                </p>
              </div>
              <Switch
                checked={source.enabled}
                onCheckedChange={() => toggleSource(source.id)}
              />
            </div>
          ))}
        </div>
      </div>
    </StepContainer>
  );
}
