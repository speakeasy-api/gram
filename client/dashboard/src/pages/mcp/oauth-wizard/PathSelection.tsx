import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import { ServerIcon, WaypointsIcon, type LucideIcon } from "lucide-react";

import { WizardContext } from "./machine";

export function PathSelection(): JSX.Element {
  const actorRef = WizardContext.useActorRef();
  const send = actorRef.send.bind(actorRef);

  return (
    <div className="space-y-4">
      <Text muted small>
        Choose how you want to configure OAuth for this MCP server.
      </Text>

      <div className="flex flex-col gap-4">
        <PathOptionCard
          title="OAuth Proxy"
          onClick={() => send({ type: "SELECT_PROXY" })}
          icon={WaypointsIcon}
        >
          <Text muted small>
            Use existing OAuth credentials from the upstream service to
            authenticate users. Best for internal MCP servers or when the
            upstream service doesn’t support MCP-native OAuth.
          </Text>
        </PathOptionCard>
        <PathOptionCard
          title="External OAuth"
          onClick={() => send({ type: "SELECT_EXTERNAL" })}
          icon={ServerIcon}
        >
          <Text muted small>
            Allow MCP clients to interact directly with an external OAuth
            provider. The external provider must support dynamic client
            registration (DCR).
          </Text>
        </PathOptionCard>
      </div>
    </div>
  );
}

function PathOptionCard(props: {
  title: string;
  onClick: () => void;
  icon: LucideIcon;
  children: React.ReactNode;
}) {
  const { icon: Icon } = props;

  return (
    <button
      type="button"
      className={cn(
        "border-border flex flex-col items-start gap-2 border p-6 text-left transition-colors",
        "hover:border-primary hover:bg-muted/50",
      )}
      onClick={props.onClick}
    >
      <div className="flex items-center gap-2">
        <Icon className="text-muted-foreground w-5" />
        <Text className="font-medium">{props.title}</Text>
      </div>
      {props.children}
    </button>
  );
}
