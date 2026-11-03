import { Badge } from "@/components/ui/Badge";
import { SimpleTooltip } from "@/components/ui/Tooltip";

type MemoryState = "remembered" | "not_summarized" | "excluded" | "personal";

const STATES: Record<
  MemoryState,
  {
    label: string;
    tooltip: string;
    variant: "success" | "neutral" | "warning";
  }
> = {
  remembered: {
    label: "In memory",
    tooltip:
      "Agents can recall this session's masked summary through Organizational Memory.",
    variant: "success",
  },
  not_summarized: {
    label: "Not yet",
    tooltip:
      "No masked summary yet. Sessions are summarized about 30 minutes after they go quiet, when memory fact extraction is on.",
    variant: "neutral",
  },
  excluded: {
    label: "Excluded",
    tooltip: "Someone kept this session out of Organizational Memory.",
    variant: "warning",
  },
  personal: {
    label: "Personal",
    tooltip:
      "Sessions from personal or unclassified AI accounts never enter Organizational Memory.",
    variant: "neutral",
  },
};

function isMemoryState(value: string | undefined): value is MemoryState {
  return value !== undefined && Object.hasOwn(STATES, value);
}

/**
 * A session's standing in Organizational Memory, with why on hover. Renders
 * nothing for a missing or unknown state.
 */
export function MemoryStateBadge({
  state,
}: {
  state: string | undefined;
}): JSX.Element | null {
  if (!isMemoryState(state)) return null;
  const config = STATES[state];
  return (
    <SimpleTooltip tooltip={config.tooltip}>
      <span className="inline-flex">
        <Badge variant={config.variant} background={state === "remembered"}>
          <Badge.Text>{config.label}</Badge.Text>
        </Badge>
      </span>
    </SimpleTooltip>
  );
}
