import { Type } from "@/components/ui/type";
import { LucideIcon } from "lucide-react";
import { ReactNode } from "react";

interface InfoFieldProps {
  icon: LucideIcon;
  label: string;
  value: ReactNode;
  className?: string;
}

export function InfoField({ icon: Icon, label, value, className }: InfoFieldProps) {
  return (
    <div className={`flex items-start gap-4 p-3 rounded-lg bg-surface-secondary/20 border border-border/50 ${className || ""}`}>
      <Icon className="h-5 w-5 text-primary shrink-0 mt-0.5" />
      <div className="flex-1 min-w-0">
        <Type small muted className="mb-1">{label}</Type>
        <Type>{value}</Type>
      </div>
    </div>
  );
}
