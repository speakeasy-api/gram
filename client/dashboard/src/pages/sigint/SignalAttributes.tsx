import {
  Select,
  SelectTrigger,
  SelectContent,
  SelectValue,
  SelectItem,
} from "@/components/ui/Select";
import { useProject } from "@/contexts/Auth";
import type { AnalyticsQueryPayload } from "@gram/client/models/components/analyticsquerypayload.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useQuery } from "@tanstack/react-query";
import { dimensionValuesQuery } from "@/pages/explore/useDimensionValues";

export function SignalAttributes({
  request,
  value,
  onGroup,
}: {
  request: AnalyticsQueryPayload;
  value: string;
  onGroup: (key: string) => void;
}): JSX.Element {
  const project = useProject();
  const client = useGramContext();
  const scope = request.signalQuery!;
  const query = useQuery(
    dimensionValuesQuery(
      client,
      project.id,
      "signals",
      "attribute_keys",
      request.from,
      request.to,
      true,
      {
        sensorId: scope.selection.sensorId,
        trigger: scope.trigger,
        runId: scope.runId,
      },
    ),
  );
  const keys = [
    ...new Set([
      "department",
      "model_family",
      "inference_provider",
      "model_name",
      ...(query.data?.values.map((item) => item.value) ?? []),
      value,
    ]),
  ].filter(Boolean);
  const names: Record<string, string> = {
    department: "Department",
    model_family: "Model vendor",
    inference_provider: "Inference provider",
    model_name: "Model",
    environment: "Environment",
    region: "Region",
  };
  return (
    <Select
      value={value ? `attribute:${value}` : "all"}
      onValueChange={(key) => onGroup(key === "all" ? "" : key.slice(10))}
    >
      <SelectTrigger className="max-w-60" aria-label="Group by">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="all">All messages</SelectItem>
        {keys.map((key) => (
          <SelectItem key={key} value={`attribute:${key}`}>
            By {names[key] ?? key}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
