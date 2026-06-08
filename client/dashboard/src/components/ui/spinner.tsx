import { cn } from "@/lib/utils";
import { Loader2 } from "lucide-react";

export function Spinner({ className }: { className?: string }): JSX.Element {
  return <Loader2 className={cn("mr-2 h-4 w-4 animate-spin", className)} />;
}
