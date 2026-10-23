import { isRelativePath } from "@/components/setup-steps/origin";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { RadioCard, RadioCardGroup } from "@/components/ui/RadioCard";
import { Text } from "@/components/ui/Text";
import { KeyRound, Waypoints } from "lucide-react";
import { useState } from "react";
import type { JSX, ReactNode } from "react";
import { CUSTOM_PLATFORM_KEY } from "./CustomSetupInstructions";
import { hasPlatformSetup } from "./platformSetups";
import type { CatalogEntry } from "./workloadCatalog";

const API_KEY_VALUE = "api-key";
// Prefixed so a catalog key can never be mistaken for the API key option.
const PLATFORM_PREFIX = "platform:";

/** Platforms whose issuer is known but whose preset has not shipped yet. */
const PLANNED = [
  { key: "github-actions", displayName: "GitHub Actions" },
  { key: "kubernetes", displayName: "Kubernetes" },
  { key: "spiffe", displayName: "SPIFFE" },
  { key: "gcp", displayName: "Google Cloud" },
];

function selectedValue(
  credential: "api-key" | "workload",
  platformKey: string | null,
): string | null {
  if (credential === "api-key") return API_KEY_VALUE;
  if (platformKey === null) return null;
  return PLATFORM_PREFIX + platformKey;
}

/** The short trade-off line under each option. */
function CredentialMeta({ children }: { children: ReactNode }): JSX.Element {
  return (
    <span className="text-muted-foreground mt-1 block font-mono text-xs">
      {children}
    </span>
  );
}

/**
 * A platform's logo in grayscale, so presets read as options in the
 * dashboard's own palette rather than as a row of brand colors.
 */
function PlatformLogo({ icon }: { icon?: string }): JSX.Element {
  if (icon !== undefined && isRelativePath(icon)) {
    return <img src={icon} alt="" className="size-5 shrink-0 grayscale" />;
  }
  return <Waypoints aria-hidden="true" className="text-foreground size-5" />;
}

/** A catalog platform this dashboard ships no working setup for. */
function UnavailablePreset({ entry }: { entry: CatalogEntry }): JSX.Element {
  return (
    <div
      aria-disabled="true"
      className="border-border flex items-start gap-3 border border-dashed p-3 opacity-60"
    >
      <PlatformLogo icon={entry.icon} />
      <span className="min-w-0 flex-1">
        <span className="flex items-center gap-2">
          <span className="truncate font-medium">{entry.displayName}</span>
          <Badge variant="neutral" size="sm">
            Unavailable
          </Badge>
        </span>
        <Text muted small className="block">
          {entry.description}
        </Text>
        <Text muted small className="block">
          Setup is unavailable. Refresh or choose another option.
        </Text>
      </span>
    </div>
  );
}

/** The platforms Speakeasy already knows the issuer for, as one-click options. */
function PlatformPresets({
  entries,
  isPending,
  isError,
  onRetry,
  canFederate,
}: {
  entries: CatalogEntry[];
  isPending: boolean;
  isError: boolean;
  onRetry: () => void;
  canFederate: boolean;
}): JSX.Element {
  if (isError) {
    return (
      <div role="alert" className="space-y-2 border p-3">
        <Text small>The platform presets could not be loaded.</Text>
        <Button variant="secondary" size="sm" onClick={onRetry}>
          Try again
        </Button>
      </div>
    );
  }
  if (isPending) return <Text muted>Loading platforms…</Text>;
  return (
    <>
      {entries.map((entry) => {
        if (!hasPlatformSetup(entry)) {
          return <UnavailablePreset key={entry.key} entry={entry} />;
        }
        return (
          <RadioCard
            key={entry.key}
            value={PLATFORM_PREFIX + entry.key}
            title={entry.displayName}
            leading={<PlatformLogo icon={entry.icon} />}
            // Trusting a platform and admitting a subject are writes against
            // the organization's workload identities.
            disabled={!canFederate}
            className="p-3"
          >
            <Text muted small>
              {entry.description}
            </Text>
            <CredentialMeta>
              No key minted · the platform's own token
            </CredentialMeta>
          </RadioCard>
        );
      })}
    </>
  );
}

/**
 * How the agent proves who it is, in order of how often each is the right
 * answer: an API key works anywhere, a preset needs no key on a platform
 * Speakeasy already knows, and a custom issuer is for a platform that mints
 * its own tokens but has no preset.
 */
export function CredentialPicker({
  credential,
  platformKey,
  entries,
  isPending,
  isError,
  onRetry,
  canFederate,
  disabled,
  onChooseApiKey,
  onChoosePlatform,
}: {
  credential: "api-key" | "workload";
  platformKey: string | null;
  entries: CatalogEntry[];
  isPending: boolean;
  isError: boolean;
  onRetry: () => void;
  canFederate: boolean;
  /** Settled: the agent already signs in with one of these. */
  disabled: boolean;
  onChooseApiKey: () => void;
  onChoosePlatform: (key: string) => void;
}): JSX.Element {
  const customChosen =
    credential === "workload" && platformKey === CUSTOM_PLATFORM_KEY;
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const planned = PLANNED.filter(
    (plan) => !entries.some((entry) => entry.key === plan.key),
  );

  return (
    <RadioCardGroup
      value={selectedValue(credential, platformKey)}
      onValueChange={(next) => {
        if (next === API_KEY_VALUE) {
          onChooseApiKey();
          return;
        }
        onChoosePlatform(next.slice(PLATFORM_PREFIX.length));
      }}
      disabled={disabled}
      className="gap-4"
    >
      <div className="grid gap-3 sm:grid-cols-2">
        <RadioCard
          value={API_KEY_VALUE}
          title="API key"
          leading={<KeyRound aria-hidden="true" className="size-5" />}
          trailing={
            <Badge variant="neutral" size="sm">
              Default
            </Badge>
          }
          className="p-3"
        >
          <Text muted small>
            Speakeasy mints a key for this agent, delivered by a one-line setup
            script or copied by hand. Works in any runtime.
          </Text>
          <CredentialMeta>
            Minted by Speakeasy · long-lived · revocable
          </CredentialMeta>
        </RadioCard>
        <PlatformPresets
          entries={entries}
          isPending={isPending}
          isError={isError}
          onRetry={onRetry}
          canFederate={canFederate}
        />
      </div>
      <Text muted small>
        <span className="text-foreground font-medium">
          Presets need no key.
        </span>{" "}
        The platform already signs a short-lived token for each run, and
        Speakeasy trusts that token instead, so there is nothing to mint, store
        or rotate.
        {planned.length > 0 &&
          ` Coming soon: ${planned.map((plan) => plan.displayName).join(", ")}.`}
        {!canFederate &&
          " Presets and custom issuers need permission to manage workload identities."}
      </Text>
      <details
        // Kept open while the custom issuer is chosen, so the choice is
        // never hidden behind a collapsed section.
        open={advancedOpen || customChosen}
        onToggle={(event) => setAdvancedOpen(event.currentTarget.open)}
      >
        <summary className="text-muted-foreground hover:text-foreground cursor-pointer text-sm">
          Advanced: trust another platform's tokens
        </summary>
        <div className="mt-3 sm:w-1/2 sm:pr-1.5">
          <RadioCard
            value={PLATFORM_PREFIX + CUSTOM_PLATFORM_KEY}
            title="Custom OIDC issuer"
            leading={<Waypoints aria-hidden="true" className="size-5" />}
            trailing={
              <Badge variant="neutral" size="sm">
                Advanced
              </Badge>
            }
            disabled={!canFederate}
            className="p-3"
          >
            <Text muted small>
              Only when your platform already mints signed OIDC tokens for its
              workloads, such as a CI provider, a cluster or your own identity
              service. If it mints none, use an API key.
            </Text>
            <CredentialMeta>
              No key minted · you enter the issuer URLs
            </CredentialMeta>
          </RadioCard>
        </div>
      </details>
    </RadioCardGroup>
  );
}
