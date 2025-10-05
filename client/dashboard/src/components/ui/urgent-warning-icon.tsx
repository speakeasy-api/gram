import { cn } from "@/lib/utils";
import { AlertTriangle } from "lucide-react";

export function UrgentWarningIcon({ className }: { className?: string }) {
  return (
    <AlertTriangle
      className={cn("w-4 h-4 text-orange-700 dark:text-orange-300", className)}
    />
  );
}
