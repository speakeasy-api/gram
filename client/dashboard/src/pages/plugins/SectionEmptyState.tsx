import { Type } from "@/components/ui/Type";
import { Stack } from "@/components/ui/Stack";

/**
 * Bordered empty-state card shared by the plugin detail page sections
 * (servers, assignments, skills), covering both "nothing here yet" and
 * "no search matches" so the two states stay visually consistent.
 */
export function SectionEmptyState({
  title,
  subtitle,
}: {
  title: string;
  subtitle?: string;
}): JSX.Element {
  return (
    <Stack
      gap={2}
      className="border-border rounded-xl border py-8"
      align="center"
      justify="center"
    >
      <Type variant="body" muted>
        {title}
      </Type>
      {subtitle && (
        <Type small muted>
          {subtitle}
        </Type>
      )}
    </Stack>
  );
}
