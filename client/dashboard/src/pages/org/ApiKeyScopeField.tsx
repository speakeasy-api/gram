import { AnyField } from "@/components/moon/any-field";
import { Badge } from "@/components/ui/Badge";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { RadioCard, RadioCardGroup } from "@/components/ui/RadioCard";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import { Check, ChevronDown, X } from "lucide-react";
import { useId, useState } from "react";

const DEFAULT_API_KEY_SCOPE = "consumer";

type ApiKeyScopeOption = {
  /** Scope value sent to the API — also the label shown in the key list. */
  value: string;
  title: string;
  tagline: string;
  grants: string[];
  excludes: string;
  /** Setup guidance that is not itself a permission. */
  note?: string;
  badge?: string;
};

// Each tagline names the integration the scope exists for, because that is what
// the person creating a key actually knows. The grants and exclusions are
// checked against the `Scope(...)` gates in server/design — both the
// declarations in security/api_key.go and the per-service and per-method gates
// that decide which endpoints a scope reaches — including the one-way
// implications (producer covers consumer and chat; agent covers agent_user).
//
// The `chat` scope is deliberately absent: nothing is provisioned against it any
// more (the assistant runtime mints its own chat credentials server-side), so
// offering it here only widens the choice. The server still honors the scope,
// so keys that already carry it keep working.
const GENERAL_SCOPE_OPTIONS: ApiKeyScopeOption[] = [
  {
    value: "consumer",
    title: "Consumer",
    tagline: "For clients that call your MCP servers and tools at runtime.",
    grants: [
      "Call MCP servers and the tools they expose",
      "Look up the tools in a toolset for an environment",
      "Read and update MCP install metadata",
    ],
    excludes: "Deployments, editing toolsets, chat transcripts",
    badge: "Default",
  },
  {
    value: "producer",
    title: "Producer",
    tagline: "For automation that sets up and updates a project, such as CI.",
    grants: [
      "Upload OpenAPI documents and trigger deployments",
      "Create and edit projects, toolsets, and MCP servers",
      "Read chat transcripts, telemetry, and risk findings",
      "Everything a Consumer key can do",
    ],
    excludes: "Sending AI traffic in, enrolling device agents",
  },
];

const PURPOSE_BUILT_SCOPE_OPTIONS: ApiKeyScopeOption[] = [
  {
    value: "hooks",
    title: "Hooks",
    tagline: "For agent plugins that report AI traffic back to Gram.",
    grants: [
      "Send hook events, logs, metrics, and traces",
      "Send skill content and feedback",
    ],
    excludes: "Reading or changing anything in a project",
  },
  {
    value: "agent",
    title: "Agent",
    tagline:
      "For rolling out the Speakeasy device agent across the organization.",
    grants: [
      "Mint the per-user key each enrolled device runs on",
      "Read a user's assigned plugins and report device scans",
    ],
    excludes: "Project setup, deployments, tool calls",
    note: "Store it in managed.json as org_token, or hand it to a developer for speakeasy enroll.",
  },
];

function ScopeDetail({
  icon,
  children,
}: {
  icon: React.ReactNode;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <li className="text-muted-foreground flex items-start gap-2 text-sm">
      <span aria-hidden className="mt-[0.2rem] shrink-0">
        {icon}
      </span>
      <span>{children}</span>
    </li>
  );
}

function ScopeOptionCard({
  option,
}: {
  option: ApiKeyScopeOption;
}): JSX.Element {
  const [detailsOpen, setDetailsOpen] = useState(false);

  return (
    <RadioCard
      value={option.value}
      title={
        <span className="flex items-center gap-2">
          {option.title}
          {option.badge ? (
            <Badge size="sm" variant="neutral">
              {option.badge}
            </Badge>
          ) : null}
        </span>
      }
    >
      <Text small>{option.tagline}</Text>
      <Collapsible open={detailsOpen} onOpenChange={setDetailsOpen}>
        <CollapsibleTrigger
          aria-label={`Access details for ${option.title}`}
          className="text-muted-foreground hover:text-foreground mt-1 flex items-center gap-1 text-xs"
        >
          Access details
          <ChevronDown
            aria-hidden
            className={cn(
              "size-3 transition-transform",
              detailsOpen && "rotate-180",
            )}
          />
        </CollapsibleTrigger>
        <CollapsibleContent>
          <ul className="mt-2 space-y-1">
            {option.grants.map((grant) => (
              <ScopeDetail key={grant} icon={<Check className="size-3.5" />}>
                {grant}
              </ScopeDetail>
            ))}
            <ScopeDetail icon={<X className="size-3.5" />}>
              {option.excludes}
            </ScopeDetail>
          </ul>
          {option.note ? (
            <Text small muted className="mt-2">
              {option.note}
            </Text>
          ) : null}
        </CollapsibleContent>
      </Collapsible>
    </RadioCard>
  );
}

function ScopeGroupHeading({ children }: { children: string }): JSX.Element {
  return <span className="text-eyebrow mt-2 first:mt-0">{children}</span>;
}

/**
 * Permission scope picker for API key creation. The scope is read off the form
 * as the `scope` field, so the values here are the API scope names verbatim.
 */
export function ApiKeyScopeField(): JSX.Element {
  // The guidance sits above the cards rather than in AnyField's `hint` slot:
  // the option list is tall enough that a trailing hint lands offscreen.
  const hintId = useId();

  return (
    <AnyField
      label="Scope"
      optionality="hidden"
      render={({ id }) => (
        <>
          <Text small muted id={hintId}>
            A key's scope is fixed once it is created. Pick the narrowest scope
            that covers the job.
          </Text>
          {/* The field's generated id belongs on the group: it is what the
              "Scope" label points at, so dropping it orphans the label. */}
          <RadioCardGroup
            id={id}
            name="scope"
            defaultValue={DEFAULT_API_KEY_SCOPE}
            aria-label="Scope"
            aria-describedby={hintId}
          >
            <ScopeGroupHeading>Platform access</ScopeGroupHeading>
            {GENERAL_SCOPE_OPTIONS.map((option) => (
              <ScopeOptionCard key={option.value} option={option} />
            ))}
            <ScopeGroupHeading>Purpose-built keys</ScopeGroupHeading>
            {PURPOSE_BUILT_SCOPE_OPTIONS.map((option) => (
              <ScopeOptionCard key={option.value} option={option} />
            ))}
          </RadioCardGroup>
        </>
      )}
    />
  );
}
