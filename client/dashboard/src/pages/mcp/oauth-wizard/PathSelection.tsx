import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import {
  ServerIcon,
  WaypointsIcon,
  ZapIcon,
  type LucideIcon,
} from "lucide-react";

import { Badge } from "@speakeasy-api/moonshine";
import { ReactNode } from "react";
import { WizardContext } from "./machine";
import type { DiscoveredOAuth } from "./machine-types";

export function PathSelection() {
  const send = WizardContext.useActorRef().send;
  const discovered = WizardContext.useSelector((s) => s.context.discovered);
  const isV21 = discovered?.version === "2.1";

  return (
    <div className="space-y-4">
      {discovered && <OAuthDetectedCallout discovered={discovered} />}

      <Type muted small>
        Choose how you want to configure OAuth for this MCP server.
      </Type>

      <div className="flex flex-col gap-4">
        {isV21 && (
          <PathOptionCard
            title="Auto-Configure"
            onClick={() => send({ type: "SELECT_PROXY_AUTO" })}
            icon={ZapIcon}
            badge={<Badge variant="information">Recommended</Badge>}
          >
            <Type muted small>
              Automatically set up OAuth Proxy based on pre-discovered details
              about this MCP server.
            </Type>
          </PathOptionCard>
        )}

        <PathOptionCard
          title="OAuth Proxy"
          onClick={() => send({ type: "SELECT_PROXY" })}
          icon={WaypointsIcon}
          badge={
            !isV21 &&
            discovered?.version === "2.0" && (
              <Badge variant="information">Recommended</Badge>
            )
          }
        >
          <Type muted small>
            Use existing OAuth credentials from the upstream service to
            authenticate users. Best for internal MCP servers or when the
            upstream service doesn’t support MCP-native OAuth.
          </Type>
        </PathOptionCard>
        <PathOptionCard
          title="External OAuth"
          onClick={() => send({ type: "SELECT_EXTERNAL" })}
          icon={ServerIcon}
        >
          <Type muted small>
            Allow MCP clients to interact directly with an external OAuth
            provider. The external provider must support dynamic client
            registration (DCR).
          </Type>
        </PathOptionCard>
      </div>
    </div>
  );
}

const OAuthDetectedCallout = ({
  discovered,
}: {
  discovered: DiscoveredOAuth;
}) => {
  const { name, version } = discovered;

  let description: ReactNode = (
    <Type muted small className="mt-1">
      We discovered OAuth {version} metadata from this server. The configuration
      will be pre-filled for either selection below.
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

function PathOptionCard(props: {
  title: string;
  badge?: React.ReactNode;
  onClick: () => void;
  icon: LucideIcon;
  children: React.ReactNode;
}) {
  const { icon: Icon } = props;

  return (
    <button
      type="button"
      className={cn(
        "border-border flex flex-col items-start gap-2 rounded-lg border p-6 text-left transition-colors",
        "hover:border-primary hover:bg-muted/50",
      )}
      onClick={props.onClick}
    >
      <div className="flex items-center gap-2">
        <Icon className="text-muted-foreground w-5" />
        <Type className="font-medium">{props.title}</Type>
      </div>
      {props.badge}
      {props.children}
    </button>
  );
}
