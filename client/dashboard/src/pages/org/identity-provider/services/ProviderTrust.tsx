import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";

import { acceptanceLabel, type ProviderView } from "./emaServiceView";

/** Whether the service has accepted this provider, as one word plus a dot. */
export function ProviderTrust({
  provider,
}: {
  provider: ProviderView;
}): JSX.Element {
  const tone = provider.lastAcceptedAt
    ? "bg-[var(--fill-success-default)]"
    : provider.usable
      ? "bg-muted-foreground/40"
      : "bg-[var(--fill-warning-default)]";
  return (
    <span className="flex min-w-0 items-baseline gap-2">
      <span
        aria-hidden="true"
        className={cn("size-2 shrink-0 translate-y-[-1px] rounded-full", tone)}
      />
      <Text small className="truncate font-medium">
        {provider.name}
      </Text>
      <Text muted small className="truncate">
        {provider.usable
          ? acceptanceLabel(provider.lastAcceptedAt)
          : provider.unavailable}
      </Text>
    </span>
  );
}
