import { StatusDot } from "@/components/ui/StatusDot";
import { Text } from "@/components/ui/Text";

import {
  acceptanceLabel,
  SPEAKEASY_DEFAULT_SCOPE,
  type ProviderView,
} from "./emaServiceView";

/** Whether the service has accepted this provider, as a dot plus a few words. */
export function ProviderTrust({
  provider,
  isDefaultOrder = false,
}: {
  provider: ProviderView;
  /** In the default order, say which servers each entry covers. */
  isDefaultOrder?: boolean;
}): JSX.Element {
  const tone = provider.lastAcceptedAt
    ? "success"
    : provider.usable
      ? "neutral"
      : "warning";
  const detail = provider.usable
    ? acceptanceLabel(provider.lastAcceptedAt)
    : provider.unavailable;
  const scope =
    !isDefaultOrder || !provider.usable
      ? undefined
      : provider.kind === "speakeasy"
        ? SPEAKEASY_DEFAULT_SCOPE.charAt(0).toLowerCase() +
          SPEAKEASY_DEFAULT_SCOPE.slice(1)
        : provider.placeholder
          ? "for servers whose people sign in through one"
          : undefined;
  return (
    <div className="flex min-w-0 flex-wrap items-baseline gap-x-2">
      <StatusDot tone={tone} className="translate-y-[-1px]" />
      <Text as="span" small className="truncate font-medium">
        {provider.name}
      </Text>
      <Text as="span" muted small className="truncate">
        {scope ? `${detail} · ${scope}` : detail}
      </Text>
    </div>
  );
}
