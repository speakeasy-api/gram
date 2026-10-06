import type { WorkloadIssuer } from "@gram/client/models/components/workloadissuer.js";
import type { WorkloadPlatform } from "@gram/client/models/components/workloadplatform.js";
import type { WorkloadPlatformBlock } from "@gram/client/models/components/workloadplatformblock.js";
import { useWorkloadPlatforms } from "@gram/client/react-query/workloadPlatforms.js";
import { useMemo } from "react";
import type {
  CatalogEntry,
  ComputedValueKey,
  SetupBlock,
  SetupDefinition,
} from "./definition";

/**
 * The catalog the server ships, as the renderer's types. The server validates
 * every entry when it loads, so this only reshapes: an empty optional field
 * becomes absent and a block keeps only the fields its type uses.
 */
export function useCatalogEntries(): {
  entries: CatalogEntry[];
  isPending: boolean;
  isError: boolean;
  refetch: () => void;
} {
  const query = useWorkloadPlatforms({}, undefined, { throwOnError: false });
  const entries = useMemo(
    () => (query.data?.platforms ?? []).map(toCatalogEntry),
    [query.data],
  );
  return {
    entries,
    isPending: query.isPending,
    isError: query.isError,
    refetch: () => void query.refetch(),
  };
}

export function toCatalogEntry(platform: WorkloadPlatform): CatalogEntry {
  return {
    key: platform.key,
    displayName: platform.displayName,
    description: platform.description,
    icon: optional(platform.icon),
    enabled: platform.enabled,
    issuer: platform.issuer,
    jwksUri: platform.jwksUri,
    variables: platform.variables.map((variable) => ({
      key: variable.key,
      tier: variable.tier,
      label: variable.label,
      help: optional(variable.help),
      placeholder: optional(variable.placeholder),
      pattern: optional(variable.pattern),
      patternMessage: optional(variable.patternMessage),
    })),
    subject: platform.subject,
    setup: toSetupDefinition(platform),
  };
}

function toSetupDefinition(
  platform: WorkloadPlatform,
): SetupDefinition | undefined {
  if (platform.steps.length === 0) {
    return undefined;
  }
  return {
    steps: platform.steps.map((step) => ({
      id: step.id,
      title: step.title,
      phase: step.phase,
      blocks: step.blocks.flatMap(toSetupBlock),
    })),
  };
}

/** A block keeps only the fields its type uses. */
function toSetupBlock(block: WorkloadPlatformBlock): SetupBlock[] {
  switch (block.type) {
    case "text":
      return [{ type: "text", markdown: block.markdown }];
    case "image":
      return [
        {
          type: "image",
          src: block.src,
          alt: block.alt,
          caption: optional(block.caption),
        },
      ];
    case "link":
      return [{ type: "link", href: block.href, label: block.label }];
    case "field":
      return [{ type: "field", variable: block.variable }];
    case "subject_rule":
      return [{ type: "subject_rule" }];
    case "agent_picker":
      return [{ type: "agent_picker" }];
    case "tags":
      return [{ type: "tags" }];
    case "computed_status":
      return [{ type: "computed_status" }];
    case "computed":
      if (block.value === "") return [];
      return [
        {
          type: "computed",
          value: block.value satisfies ComputedValueKey,
          label: block.label,
          help: optional(block.help),
        },
      ];
  }
}

function optional(value: string): string | undefined {
  return value === "" ? undefined : value;
}

/**
 * The trusted platform an entry corresponds to, if the organization trusts it.
 * Matched on the issuer and JWKS URLs until rows carry the catalog key they
 * were created from (AIM-374). The issuer alone is not enough: two rows may
 * share an issuer URL with different key URLs, and only the one fetching the
 * entry's keys verifies the platform's tokens.
 */
export function connectedIssuer(
  entry: CatalogEntry,
  issuers: WorkloadIssuer[],
): WorkloadIssuer | undefined {
  return issuers.find(
    (issuer) =>
      issuer.issuer === entry.issuer.value &&
      issuer.jwksUri === entry.jwksUri.value,
  );
}
