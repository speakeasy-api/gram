import { Text } from "@/components/ui/Text";
import type { ReactNode } from "react";

export function SourceInfoTable({
  children,
}: {
  children: ReactNode;
}): JSX.Element {
  return <div className="divide-y border">{children}</div>;
}

export function SourceInfoRow({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <div className="flex items-center justify-between px-3 py-2.5">
      <Text muted small>
        {label}
      </Text>
      <div className="text-right">{children}</div>
    </div>
  );
}
