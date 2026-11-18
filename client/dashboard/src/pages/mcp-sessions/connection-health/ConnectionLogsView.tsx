import { InlineEmptyState } from "@/components/inline-empty-state";

export function ConnectionLogsView({
  providerId: _providerId,
}: {
  providerId: string;
}): JSX.Element {
  return <InlineEmptyState icon="scroll-text" heading="Connection Logs" />;
}
