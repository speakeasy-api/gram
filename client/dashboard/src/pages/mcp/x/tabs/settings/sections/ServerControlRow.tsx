import { Text } from "@/components/ui/Text";

/** One action in a settings panel: what it does on the left, its button on the right. */
export function ServerControlRow({
  title,
  description,
  children,
}: {
  title: string;
  description: string;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <div className="flex flex-col gap-3 py-4 first:pt-0 last:pb-0 sm:flex-row sm:items-center sm:justify-between">
      <div className="min-w-0 space-y-1">
        <Text small className="font-medium">
          {title}
        </Text>
        <Text muted small className="max-w-2xl">
          {description}
        </Text>
      </div>
      <div className="flex shrink-0 items-center gap-2">{children}</div>
    </div>
  );
}
