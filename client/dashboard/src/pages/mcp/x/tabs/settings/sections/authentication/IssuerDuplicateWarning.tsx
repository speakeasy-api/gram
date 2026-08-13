import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import type {
  IssuerDuplicateScope,
  RemoteSessionIssuerDuplicateMatch,
} from "./useIssuerDuplicatePreflight";

// What reusing the existing record gives the operator instead of adding their
// own. Phrased as what they gain, because duplicating is a legitimate choice and
// the warning is here to inform it rather than to scold.
//
// Depends on who is asking as well as what matched. A platform-level match is
// something a tenant INHERITS and a platform administrator OWNS, so telling the
// curator their catalog is "maintained for you" reads as nonsense on the one
// surface where they are the maintainer.
function tierRationale(
  match: RemoteSessionIssuerDuplicateMatch,
  viewerScope: IssuerDuplicateScope,
): string {
  if (viewerScope === "platform") {
    return "The catalog already covers this authorization server. A second entry gives every tenant two records for one provider with nothing to tell them apart.";
  }

  switch (match.tier) {
    case "platform-level":
      return "Curated by Speakeasy and kept up to date centrally, so its metadata and setup documentation are maintained for you.";
    case "organization-level":
      return "Already available to every project in your organization, so reusing it keeps one record to maintain instead of two.";
    case "project-specific":
      // A project match reaches the organization surfaces too, where it belongs
      // to some OTHER project and "already configured here" would be false.
      // projectName is populated exactly when the caller might not be able to
      // place the match, so its presence is what distinguishes the two cases.
      if (match.projectName) {
        return "Consolidating onto one record avoids each project maintaining its own metadata and documentation for the same provider.";
      }
      return "Already configured here, so a second record would double the metadata and documentation you maintain.";
  }
}

// tierLabel names where a match lives. A project-specific match names the
// project when the caller was told which one, since "Project" alone is not
// placeable for an organization administrator looking across many.
function tierLabel(match: RemoteSessionIssuerDuplicateMatch): string {
  switch (match.tier) {
    case "platform-level":
      return "Platform catalog";
    case "organization-level":
      return "Organization";
    case "project-specific":
      return match.projectName
        ? `Project ${match.projectName}`
        : "This project";
  }
}

// matchDisplayName mirrors how issuers are named everywhere else: the display
// name when it has one, otherwise the slug.
function matchDisplayName(match: RemoteSessionIssuerDuplicateMatch): string {
  return match.name.trim() || match.slug;
}

// otherMatchesSentence summarizes the matches beyond the one the warning leads
// with.
//
// Deliberately unnumbered. The server truncates per tier, so a count taken from
// this list would understate the truth as soon as more than a few records share
// a URL, and the result carries no total to correct it with. Naming them without
// counting them is the claim the data actually supports.
function otherMatchesSentence(
  others: RemoteSessionIssuerDuplicateMatch[],
): string {
  const named = others
    .map((match) => `${matchDisplayName(match)} (${tierLabel(match)})`)
    .join(", ");

  if (others.length === 1) {
    return `Another identity provider also uses it: ${named}.`;
  }
  return `Other identity providers also use it: ${named}.`;
}

// IssuerDuplicateWarning tells an operator that something they can already see
// describes the issuer URL they just entered.
//
// It never blocks. Duplicating an issuer URL is supported and sometimes
// deliberate — a tenant may want its own record precisely so it can attach
// different documentation, branding or scopes — so this is an advisory Alert
// beside the field, not a validation error on it.
//
// `onUseExisting`, where a surface can offer it, turns the warning into the
// one-click fix. Surfaces that have no way to adopt an existing record instead
// of creating one omit it and render the link alone.
export function IssuerDuplicateWarning({
  matches,
  viewerScope,
  onUseExisting,
  renderLink,
}: {
  matches: RemoteSessionIssuerDuplicateMatch[];
  // The tier the operator is working at, which is not the tier of the match.
  // Required rather than defaulted so a new surface has to state it instead of
  // silently inheriting tenant-facing copy.
  viewerScope: IssuerDuplicateScope;
  onUseExisting?: (match: RemoteSessionIssuerDuplicateMatch) => void;
  renderLink?: (match: RemoteSessionIssuerDuplicateMatch) => JSX.Element;
}): JSX.Element | null {
  if (matches.length === 0) {
    return null;
  }

  // Matches arrive in resolution order, narrowest tier first, so the first is
  // the record this caller would resolve the URL to today. That makes it the
  // one to lead with and the one a reuse action should adopt.
  const [primary, ...rest] = matches;
  if (!primary) {
    return null;
  }

  const actions = [
    onUseExisting ? (
      <Button
        key="use-existing"
        variant="secondary"
        onClick={() => onUseExisting(primary)}
      >
        <Button.Text>Use {matchDisplayName(primary)} instead</Button.Text>
      </Button>
    ) : null,
    renderLink?.(primary) ?? null,
  ].filter(Boolean);

  return (
    <Alert variant="warning" dismissible={false}>
      <Stack gap={2}>
        <Text small>
          <strong>{matchDisplayName(primary)}</strong> ({tierLabel(primary)})
          already uses this issuer URL. {tierRationale(primary, viewerScope)}
        </Text>

        {rest.length > 0 && (
          <Text muted small>
            {otherMatchesSentence(rest)}
          </Text>
        )}

        <Text muted small>
          {viewerScope === "platform"
            ? "You can still continue if the two entries are meant to differ."
            : "You can still continue. Add your own record when you need different documentation, branding or scopes than the one above."}
        </Text>

        {actions.length > 0 && (
          <Stack direction="horizontal" gap={2}>
            {actions}
          </Stack>
        )}
      </Stack>
    </Alert>
  );
}
