import { Alert } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { RadioCard, RadioCardGroup } from "@/components/ui/RadioCard";
import { Text } from "@/components/ui/Text";
import { Info } from "lucide-react";
import { useState } from "react";
import { AgentIdentityRow } from "./AgentIdentityRow";
import { IdentityExplainerCallout } from "./IdentityExplainer";
import { identityModeCards } from "./identityModes";
import type { AgentCredentialFields } from "./useAgentCredentialDraft";

export type CreationIdentityMode = "user" | "agent" | "none";

/**
 * The identity decision at creation time, shared by the catalog Add-server
 * dialog and Add-by-URL. AIM-230 asks both to present the same three cards
 * once the Remote MCP probe has answered, with User Identity preselected and
 * badged Default when the upstream advertises OAuth.
 */
export function CreationIdentityChoice({
  value,
  onChange,
  credential,
  upstreamName,
  advertisesOAuth,
  authenticationRequired,
  canCreateIdentity,
  rbacLoading,
}: {
  value: CreationIdentityMode;
  onChange: (mode: CreationIdentityMode) => void;
  credential: AgentCredentialFields;
  upstreamName: string;
  /** The probe saw an OAuth challenge, so User Identity is the default. */
  advertisesOAuth: boolean;
  authenticationRequired: boolean;
  canCreateIdentity: boolean;
  rbacLoading: boolean;
}): JSX.Element {
  const [explainerOpen, setExplainerOpen] = useState(false);

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <Text small className="font-medium">
          Identity
        </Text>
        <button
          type="button"
          onClick={() => setExplainerOpen((open) => !open)}
          aria-label="What do these identity modes mean?"
          aria-expanded={explainerOpen}
          className="text-muted-foreground hover:text-foreground"
        >
          <Info aria-hidden="true" className="size-3.5" />
        </button>
      </div>
      <Text muted small className="block">
        {advertisesOAuth
          ? `${upstreamName} advertises OAuth sign-in, so User Identity is the default.`
          : `How callers are identified to ${upstreamName}.`}
      </Text>

      {explainerOpen ? (
        <IdentityExplainerCallout onDismiss={() => setExplainerOpen(false)} />
      ) : null}

      <RadioCardGroup
        size="sm"
        showIndicator={false}
        value={value}
        onValueChange={(next) => onChange(next as CreationIdentityMode)}
      >
        {identityModeCards(upstreamName).map((card) => (
          <RadioCard
            key={card.value}
            value={card.value}
            disabled={card.value === "user" && !canCreateIdentity}
            leading={card.icon}
            title={
              card.value === "user" && advertisesOAuth ? (
                <span className="flex items-center gap-2">
                  {card.title}
                  <Badge variant="information" size="sm">
                    <Badge.Text>Default</Badge.Text>
                  </Badge>
                </span>
              ) : (
                card.title
              )
            }
          >
            {card.description}
          </RadioCard>
        ))}
      </RadioCardGroup>

      {value === "agent" ? (
        <AgentIdentityRow
          draft={credential}
          disabled={false}
          upstreamName={upstreamName}
        />
      ) : null}

      {value === "none" && authenticationRequired ? (
        <Alert variant="warning" dismissible={false}>
          This server requires authentication. No Identity may leave it unable
          to serve requests.
        </Alert>
      ) : null}

      {value === "user" && !rbacLoading && !canCreateIdentity ? (
        <Alert variant="warning" dismissible={false}>
          User Identity creates an identity provider and requires project:write.
          Choose another identity mode or ask a project administrator.
        </Alert>
      ) : null}
    </div>
  );
}
