import { Alert } from "@/components/ui/Alert";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import type { IssuerFieldMismatch } from "@gram/client/models/components/issuerfieldmismatch.js";
import {
  isListMismatch,
  listMismatchDelta,
  mismatchFieldNames,
  mismatchValueLabel,
  warningSentence,
} from "./issuerMismatches";

// Everything these render sits inside an Alert, which colors its whole subtree
// to match its severity. So they style with plain elements and inherit that
// color, rather than reaching for Text or a border token: Text sets its own
// foreground unless it is muted or destructive, and would silently repaint an
// error alert's body neutral while the alert's own border stayed red.

// EntryList shows the entries on one side of a list-valued difference, or the
// entries a migration adds or drops. Entries are rendered as individual chips
// rather than a joined sentence so a long scope list stays scannable.
//
// Deliberately not the Badge component: Badge is how this dialog's own table and
// headers label an identity provider's tenancy tier, and reusing it for OAuth
// scopes would read as the same kind of thing.
function EntryList({
  label,
  entries,
}: {
  label: string;
  entries: string[];
}): JSX.Element {
  return (
    <div className="flex flex-wrap items-baseline gap-1">
      <span className="text-sm opacity-70">{label}:</span>
      {entries.length === 0 ? (
        <span className="text-sm opacity-70">none</span>
      ) : (
        entries.map((entry, index) => (
          <code
            key={`${entry}-${index}`}
            className="rounded border border-current/40 px-1 font-mono text-xs break-all"
          >
            {entry}
          </code>
        ))
      )}
    </div>
  );
}

// ValueRow labels one side of a scalar difference. The value breaks anywhere it
// has to: these are endpoint URLs with no spaces in them, and letting one
// overflow the alert would hide the very difference the row exists to show.
function ValueRow({
  label,
  value,
}: {
  label: string;
  value: string | undefined;
}): JSX.Element {
  return (
    <div className="grid grid-cols-[3.5rem_1fr] items-baseline gap-1">
      <span className="text-sm opacity-70">{label}</span>
      <div className="font-mono text-sm break-all">
        {mismatchValueLabel(value)}
      </div>
    </div>
  );
}

// ScalarMismatch shows both sides of a field that holds a single value, so an
// admin can see how far apart the two providers are rather than only that they
// disagree.
//
// The two values are stacked rather than joined into one line. They are long
// enough that an inline pair wraps mid-URL, which splits the host across two
// lines and makes two endpoints that differ only in their host hard to tell
// apart at a glance.
function ScalarMismatch({
  mismatch,
}: {
  mismatch: IssuerFieldMismatch;
}): JSX.Element {
  return (
    <div className="flex flex-col gap-0.5">
      <div className="font-mono text-sm">{mismatch.field}</div>
      <ValueRow label="Source" value={mismatch.sourceValue} />
      <ValueRow label="Target" value={mismatch.targetValue} />
    </div>
  );
}

// WarningDetail describes one non-blocking difference and, for a list-valued
// field, what the migrated clients actually gain and lose. A list difference
// that turns out to add and remove nothing has no delta to draw, so both lists
// are shown whole rather than leaving the sentence standing alone with no
// values under it.
function WarningDetail({
  mismatch,
}: {
  mismatch: IssuerFieldMismatch;
}): JSX.Element {
  const { added, removed } = listMismatchDelta(mismatch);
  const hasDelta = added.length > 0 || removed.length > 0;

  return (
    <Stack gap={1}>
      <p className="text-sm">{warningSentence(mismatch)}</p>

      {isListMismatch(mismatch) &&
        (hasDelta ? (
          <Stack gap={1}>
            {added.length > 0 && <EntryList label="Added" entries={added} />}
            {removed.length > 0 && (
              <EntryList label="Dropped" entries={removed} />
            )}
          </Stack>
        ) : (
          <Stack gap={1}>
            <EntryList label="Source" entries={mismatch.sourceValues ?? []} />
            <EntryList label="Target" entries={mismatch.targetValues ?? []} />
          </Stack>
        ))}
    </Stack>
  );
}

// MigrateImpact renders the server's authoritative preflight: what moves, what
// blocks the migration, and what changes without blocking it. Blockers are
// rendered as errors because the mutation rejects them; warnings are rendered as
// warnings because the target's values simply become authoritative.
//
// The server decides which fields differ and supplies both sides' values; the
// wording is here. Recomputing which fields differ on the client would be a
// second implementation of a comparison the mutation also runs — the issuer
// identifier is compared canonically, the endpoints literally, and an unset
// value is not an empty one — and the dialog an admin confirms must not be able
// to disagree with the mutation about what is wrong.
//
// Shared by the organization consolidation dialog and the platform-admin
// convergence dialog. Both call preflight endpoints that return the same blocker
// set, and the two surfaces must not describe the same blocker differently: an
// admin who sees one wording in the tenant UI and another in the platform UI has
// no way to tell whether they are looking at the same problem.
export function MigrateImpact({
  isLoading,
  hasFailed,
  clientCount,
  mcpServerNames,
  endpointMismatches,
  conflictingMcpServerNames,
  warnings,
}: {
  isLoading: boolean;
  hasFailed: boolean;
  clientCount: number | undefined;
  mcpServerNames: string[] | undefined;
  endpointMismatches: IssuerFieldMismatch[] | undefined;
  conflictingMcpServerNames: string[] | undefined;
  warnings: IssuerFieldMismatch[] | undefined;
}): JSX.Element {
  if (isLoading) {
    return (
      <Text small muted>
        Checking impact…
      </Text>
    );
  }

  // Without a preflight there is nothing trustworthy to show. Say so rather than
  // rendering a zero impact summary that reads like a clean migration.
  if (hasFailed) {
    return (
      <Alert variant="error" dismissible={false}>
        Could not check the impact of this migration. Try again.
      </Alert>
    );
  }

  // An unresolved preflight is not a clean one. A query that is paused or
  // otherwise settled without data reports neither loading nor error, and
  // defaulting the count to zero there would render "0 clients move" with no
  // blockers, which reads as a safe migration on data nobody has.
  if (clientCount === undefined) {
    return (
      <Text small muted>
        Checking impact…
      </Text>
    );
  }

  const count = clientCount;

  return (
    <Stack gap={2}>
      <Text small muted>
        {count} {count === 1 ? "client moves" : "clients move"} to the target
        provider.
        {mcpServerNames && mcpServerNames.length > 0
          ? ` Affected MCP servers: ${mcpServerNames.join(", ")}.`
          : ""}
      </Text>

      {endpointMismatches && endpointMismatches.length > 0 && (
        <Alert variant="error" dismissible={false} alignTop>
          <Stack gap={2}>
            <p className="text-sm">
              {`These providers describe different authorization servers (${mismatchFieldNames(endpointMismatches).join(", ")} differ). Consolidating them would break existing sessions.`}
            </p>
            <Stack gap={1}>
              {endpointMismatches.map((mismatch) => (
                <ScalarMismatch key={mismatch.field} mismatch={mismatch} />
              ))}
            </Stack>
          </Stack>
        </Alert>
      )}

      {conflictingMcpServerNames && conflictingMcpServerNames.length > 0 && (
        <Alert variant="error" dismissible={false}>
          {`Both providers already have a client on these MCP servers: ${conflictingMcpServerNames.join(", ")}. Remove one client per server, then try again.`}
        </Alert>
      )}

      {warnings && warnings.length > 0 && (
        <Alert variant="warning" dismissible={false} alignTop>
          <Stack gap={2}>
            {warnings.map((mismatch) => (
              <WarningDetail key={mismatch.field} mismatch={mismatch} />
            ))}
          </Stack>
        </Alert>
      )}
    </Stack>
  );
}
