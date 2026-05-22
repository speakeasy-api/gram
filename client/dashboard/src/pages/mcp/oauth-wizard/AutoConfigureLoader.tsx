import { Type } from "@/components/ui/type";
import { Loader2 } from "lucide-react";

export function AutoConfigureLoader() {
  return (
    <div className="flex flex-col items-center justify-center gap-4 py-12">
      <Loader2 className="text-muted-foreground h-12 w-12 animate-spin" />
      <Type className="text-center text-lg font-medium">
        Setting up OAuth Proxy...
      </Type>
      <Type muted small className="max-w-md text-center">
        Registering Gram with the upstream OAuth provider and storing the
        returned credentials.
      </Type>
    </div>
  );
}
