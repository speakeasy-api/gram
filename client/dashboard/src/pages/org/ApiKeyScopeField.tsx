import { AnyField } from "@/components/moon/any-field";
import { Badge } from "@/components/ui/Badge";
import { RadioCard, RadioCardGroup } from "@/components/ui/RadioCard";
import { Text } from "@/components/ui/Text";
import { Check, X } from "lucide-react";
import { useId } from "react";

export const DEFAULT_API_KEY_SCOPE = "consumer";

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

// Wording mirrors the scope descriptions declared in
// server/design/security/api_key.go, including the one-way implications
// (producer covers consumer and chat; agent covers agent_user).
const GENERAL_SCOPE_OPTIONS: ApiKeyScopeOption[] = [
  {
    value: "consumer",
    title: "Consumer",
    tagline: "Use what is already set up, without changing it.",
    grants: [
      "Call MCP servers and the tools they expose",
      "Read toolsets, servers, install metadata, and roles",
    ],
    excludes: "Deployments, configuration changes, conversation content",
    badge: "Default",
  },
  {
    value: "producer",
    title: "Producer",
    tagline:
      "Manage a project end to end. Covers everything Consumer and Chat allow.",
    grants: [
      "Upload OpenAPI documents and trigger deployments",
      "Create and edit projects, toolsets, and MCP servers",
      "Read chat transcripts, telemetry, and risk findings",
    ],
    excludes: "AI traffic ingestion, device agent enrollment",
  },
];

const PURPOSE_BUILT_SCOPE_OPTIONS: ApiKeyScopeOption[] = [
  {
    value: "chat",
    title: "Chat",
    tagline: "Model access for chat clients.",
    grants: ["Start chat sessions and run agent workflows"],
    excludes: "Project setup, deployments, tool management",
  },
  {
    value: "hooks",
    title: "Hooks",
    tagline: "Ingestion only, for AI traffic sent from agents and plugins.",
    grants: ["Send hook events and OpenTelemetry data"],
    excludes: "Reading or changing any project resource",
  },
  {
    value: "agent",
    title: "Agent",
    tagline: "Organization install credential for the Speakeasy device agent.",
    grants: [
      "Fetch a user's assigned plugins",
      "Exchange for per-user device agent keys",
    ],
    excludes: "Project setup, deployments, tool management",
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
      render={() => (
        <>
          <Text small muted id={hintId}>
            A key's scope is fixed once it is created. Pick the narrowest scope
            that covers the job.
          </Text>
          <RadioCardGroup
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
