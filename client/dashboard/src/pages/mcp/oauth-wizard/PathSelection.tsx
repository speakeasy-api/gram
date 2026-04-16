import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import { Globe, LockIcon } from "lucide-react";

import { Badge } from "@speakeasy-api/moonshine";
import { ReactNode } from "react";
import type { DiscoveredOAuth, WizardDispatch } from "./types";

export function PathSelection({
  discoveredOAuth,
  dispatch,
}: {
  discoveredOAuth: DiscoveredOAuth | null;
  dispatch: WizardDispatch;
}) {
  return (
    <div className="space-y-4">
      {discoveredOAuth && (
        <OAuthDetectedCallout discoveredOAuth={discoveredOAuth} />
      )}

      <Type muted small>
        Choose how you want to configure OAuth for this MCP server.
      </Type>

      <div className="grid grid-cols-2 gap-4">
        <button
          type="button"
          className={cn(
            "border-border flex flex-col items-start gap-2 rounded-lg border p-6 text-left transition-colors",
            "hover:border-primary hover:bg-muted/50",
          )}
          onClick={() => dispatch({ type: "SELECT_PROXY", discoveredOAuth })}
        >
          <LockIcon className="text-muted-foreground h-6 w-6" />
          <Type className="font-medium">OAuth Proxy</Type>
          <Badge variant="neutral">Recommended</Badge>
          <Type muted small>
            For internal servers that don't natively support MCP OAuth. Gram
            proxies OAuth on behalf of your server.
          </Type>
        </button>
        <button
          type="button"
          className={cn(
            "border-border flex flex-col items-start gap-2 rounded-lg border p-6 text-left transition-colors",
            "hover:border-primary hover:bg-muted/50",
          )}
          onClick={() =>
            dispatch({
              type: "SELECT_EXTERNAL",
              discoveredOAuth:
                discoveredOAuth?.version === "2.1" ? discoveredOAuth : null,
            })
          }
        >
          <Globe className="text-muted-foreground h-6 w-6" />
          <Type className="font-medium">External OAuth</Type>
          <Type muted small>
            For APIs that meet the MCP OAuth spec. Uses authorization code flow
            with your external authorization server.
          </Type>
        </button>
      </div>
    </div>
  );
}

const OAuthDetectedCallout = ({
  discoveredOAuth,
}: {
  discoveredOAuth: DiscoveredOAuth;
}) => {
  const { name, version } = discoveredOAuth;

  let description: ReactNode = (
    <Type muted small className="mt-1">
      We discovered OAuth {version} metadata from this server. The
      configuration will be pre-filled for either selection below.
    </Type>
  );
  if (version == "2.0") {
    description = (
      <Type muted small className="mt-1">
        We discovered OAuth 2.0 metadata from this server. These details will be
        pre-filled for the OAuth Proxy configuration below.
      </Type>
    );
  }

  return (
    <div className="border-border bg-muted/50 flex items-start justify-between gap-4 rounded-md border p-4">
      <div>
        <Type small className="font-medium">
          OAuth detected from {name}
        </Type>
        {description}
      </div>
    </div>
  );
};
