import { Type } from "@/components/ui/type";
import type { ReactNode } from "react";

export function SourceInfoTable({ children }: { children: ReactNode }) {
  return <div className="divide-y rounded-lg border">{children}</div>;
}

export function SourceInfoRow({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}) {
  return (
    <div className="flex items-center justify-between px-3 py-2.5">
      <Type muted small>
        {label}
      </Type>
      <div className="text-right">{children}</div>
    </div>
  );
}
