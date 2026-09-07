import { Text } from "@/components/ui/Text";

/**
 * Shared layout primitives for the assistant detail side panel: a titled
 * section with an optional action + empty state, and a label/value row.
 */
export function Section({
  title,
  children,
  empty,
  isEmpty,
  action,
}: {
  title: string;
  children: React.ReactNode;
  empty?: string;
  isEmpty?: boolean;
  action?: React.ReactNode;
}): JSX.Element {
  return (
    <div>
      <div className="mb-2 flex items-center justify-between gap-2">
        <div className="text-eyebrow">{title}</div>
        {action}
      </div>
      {isEmpty && empty ? (
        <Text small muted>
          {empty}
        </Text>
      ) : (
        children
      )}
    </div>
  );
}

export function Row({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <div className="flex items-center justify-between py-1">
      <Text small muted>
        {label}
      </Text>
      <div>{children}</div>
    </div>
  );
}
