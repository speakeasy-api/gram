import { Type } from "@/components/ui/type";
import { Loader2 } from "lucide-react";

export function AutoConfigureLoader({
  mode = "proxy",
}: {
  mode?: "proxy" | "user-sessions";
}) {
  return (
    <div className="flex flex-col items-center justify-center gap-4 py-12">
      <Loader2 className="text-muted-foreground h-12 w-12 animate-spin" />
      <Type className="text-center text-lg font-medium">
        {mode === "user-sessions"
          ? "Setting up user sessions..."
          : "Setting up OAuth Proxy..."}
      </Type>
      <Type muted small className="max-w-md text-center">
        {mode === "user-sessions"
          ? "Registering Gram with the upstream OAuth provider and linking the issuer to this toolset."
          : "Registering Gram with the upstream OAuth provider and storing the returned credentials."}
      </Type>
    </div>
  );
}
