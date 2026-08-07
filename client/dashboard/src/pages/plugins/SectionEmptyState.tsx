import { Text } from "@/components/ui/Text";
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
      className="border-border border py-8"
      align="center"
      justify="center"
    >
      <Text variant="body" muted>
        {title}
      </Text>
      {subtitle && (
        <Text small muted>
          {subtitle}
        </Text>
      )}
    </Stack>
  );
}
