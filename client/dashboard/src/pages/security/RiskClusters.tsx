import { Expandable } from "@/components/expandable";
import { Badge } from "@/components/ui/badge";
import { Slider } from "@/components/ui/slider";
import { Switch } from "@/components/ui/switch";
import { useSdkClient } from "@/contexts/Sdk";
import { cn } from "@/lib/utils";
import type {
  RiskFindingCluster,
  RiskResult,
} from "@gram/client/models/components";
import { Button, Icon } from "@speakeasy-api/moonshine";
import { useQuery } from "@tanstack/react-query";
import { RefreshCw } from "lucide-react";
import { useState } from "react";
import { CategoryLabel, MaskedMatch, RuleLabel } from "./risk-ui";

// EXPERIMENTAL PoC view: presents risk findings grouped into semantic clusters
// (server-side embedding clustering) instead of a flat list, alongside the
// deterministic source|rule|match group count for comparison. See
// /home/vgd/.claude/plans/study-our-risk-policy-snuggly-hanrahan.md.
export function RiskClusters({
  onSelectChat,
}: {
  onSelectChat: (chatId: string | null) => void;
}): JSX.Element {
  const client = useSdkClient();
  const [threshold, setThreshold] = useState(0.84);
  const [includeRule, setIncludeRule] = useState(false);

  const query = useQuery({
    queryKey: ["risk", "results", "cluster", threshold, includeRule],
    queryFn: () =>
      client.risk.results.cluster({ threshold, includeRule, limit: 3000 }),
  });

  const data = query.data;
  const clusters = data?.clusters ?? [];

  return (
    <div className="space-y-4 p-6">
      <div className="bg-muted/20 flex flex-wrap items-center gap-x-6 gap-y-3 rounded-lg border px-4 py-3">
        <div className="flex w-64 items-center gap-3">
          <span className="text-muted-foreground shrink-0 text-xs font-medium">
            Threshold
          </span>
          <Slider
            min={0.7}
            max={0.95}
            step={0.01}
            value={threshold}
            onChange={setThreshold}
          />
          <span className="w-9 shrink-0 text-right font-mono text-xs tabular-nums">
            {threshold.toFixed(2)}
          </span>
        </div>
        <label className="flex cursor-pointer items-center gap-2">
          <Switch
            checked={includeRule}
            onCheckedChange={setIncludeRule}
            aria-label="Separate by detector type"
          />
          <span className="text-sm">Separate by detector type</span>
        </label>
        <Button
          variant="secondary"
          size="sm"
          onClick={() => void query.refetch()}
          disabled={query.isFetching}
        >
          <Button.LeftIcon>
            <RefreshCw
              className={cn("h-4 w-4", query.isFetching && "animate-spin")}
            />
          </Button.LeftIcon>
          <Button.Text>Refresh</Button.Text>
        </Button>
      </div>

      {data ? (
        <div className="bg-background flex flex-wrap items-baseline gap-x-2 gap-y-1 rounded-lg border px-4 py-3 text-sm">
          <span className="text-2xl font-semibold tabular-nums">
            {data.semanticClusterCount}
          </span>
          <span className="text-muted-foreground">semantic clusters from</span>
          <span className="font-semibold tabular-nums">
            {data.totalFindings}
          </span>
          <span className="text-muted-foreground">
            findings — the flat list would show
          </span>
          <span className="font-semibold tabular-nums">
            {data.baselineGroupCount}
          </span>
          <span className="text-muted-foreground">
            deterministic source|rule|match groups. Mode: {data.embedMode}.
          </span>
        </div>
      ) : null}

      {query.isLoading ? (
        <div className="text-muted-foreground flex items-center justify-center gap-2 py-12">
          <Icon name="loader-circle" className="size-5 animate-spin" />
          <span>
            Clustering findings (embedding + labeling, may take a few seconds)…
          </span>
        </div>
      ) : null}

      {query.error ? (
        <div className="flex flex-col items-center gap-3 py-12 text-center">
          <Icon name="circle-alert" className="text-destructive size-6" />
          <span className="text-foreground font-medium">
            Error clustering findings
          </span>
          <span className="text-muted-foreground max-w-md text-sm">
            {(query.error as Error).message}
          </span>
        </div>
      ) : null}

      {data && clusters.length === 0 && !query.isLoading ? (
        <div className="text-muted-foreground py-12 text-center text-sm">
          No findings to cluster.
        </div>
      ) : null}

      <div className="space-y-2">
        {clusters.map((cluster, i) => (
          <ClusterGroup
            key={cluster.id}
            cluster={cluster}
            defaultExpanded={i === 0}
            onSelectChat={onSelectChat}
          />
        ))}
      </div>
    </div>
  );
}

function ClusterGroup({
  cluster,
  defaultExpanded,
  onSelectChat,
}: {
  cluster: RiskFindingCluster;
  defaultExpanded: boolean;
  onSelectChat: (chatId: string | null) => void;
}): JSX.Element {
  return (
    <Expandable defaultExpanded={defaultExpanded} className="rounded-md">
      <Expandable.Trigger className="bg-muted/20 hover:bg-muted/40">
        <div className="flex min-w-0 flex-1 flex-wrap items-center gap-2 text-left">
          <span className="truncate font-medium">
            {cluster.label ?? "Cluster"}
          </span>
          <Badge variant="secondary">{cluster.count} findings</Badge>
          <Badge variant="outline">{cluster.distinctChats} chats</Badge>
          {typeof cluster.baselineGroupCount === "number" ? (
            <Badge
              variant="outline"
              tooltip="Distinct deterministic source|rule|match groups this one semantic cluster collapses"
            >
              collapses {cluster.baselineGroupCount} flat groups
            </Badge>
          ) : null}
          {(cluster.sources ?? []).map((s) => (
            <Badge key={s} variant="outline" className="font-mono">
              {s}
            </Badge>
          ))}
        </div>
      </Expandable.Trigger>
      <Expandable.Content className="h-auto max-h-96 p-0">
        {cluster.description ? (
          <p className="text-muted-foreground border-b px-4 py-2 text-xs">
            {cluster.description}
          </p>
        ) : null}
        <div className="divide-y">
          {cluster.members.map((m) => (
            <ClusterMemberRow
              key={m.id}
              result={m}
              onSelectChat={onSelectChat}
            />
          ))}
        </div>
      </Expandable.Content>
    </Expandable>
  );
}

function ClusterMemberRow({
  result,
  onSelectChat,
}: {
  result: RiskResult;
  onSelectChat: (chatId: string | null) => void;
}): JSX.Element {
  const isShadowMCP = result.source === "shadow_mcp";
  return (
    <div
      role={result.chatId ? "button" : undefined}
      tabIndex={result.chatId ? 0 : undefined}
      onClick={() => {
        if (result.chatId) onSelectChat(result.chatId);
      }}
      onKeyDown={(e) => {
        if (!result.chatId) return;
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onSelectChat(result.chatId);
        }
      }}
      className={cn(
        "hover:bg-muted/30 grid grid-cols-[minmax(0,0.7fr)_minmax(0,0.8fr)_minmax(0,1fr)_minmax(0,1fr)_150px] items-center gap-3 px-4 py-2 text-sm",
        result.chatId && "cursor-pointer",
      )}
    >
      <div className="min-w-0 truncate">
        <CategoryLabel source={result.source} ruleId={result.ruleId} />
      </div>
      <div className="min-w-0 truncate">
        <RuleLabel source={result.source} ruleId={result.ruleId} />
      </div>
      <div className="min-w-0 truncate">{result.chatTitle ?? "Untitled"}</div>
      <div className="min-w-0 truncate">
        {isShadowMCP && result.match ? (
          <span className="font-mono text-xs" title={result.match}>
            {result.match}
          </span>
        ) : (
          <MaskedMatch value={result.match} />
        )}
      </div>
      <div className="text-muted-foreground min-w-0 truncate font-mono text-xs">
        {result.createdAt ? new Date(result.createdAt).toLocaleString() : "-"}
      </div>
    </div>
  );
}
