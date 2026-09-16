import { useId, useRef, useState, type JSX, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { useConfirmDialog } from "@/components/ConfirmDialog";
import { CopyValue } from "@/components/CopyValue";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useOnUnmount } from "@/hooks/useOnUnmount";
import { invalidateOrganizationDirectoryHandoff } from "@/lib/adminQueries";
import {
  clearOrganizationDirectoryHandoff,
  organizationDirectoryHandoffQuery,
  setOrganizationDirectoryHandoff,
} from "@/lib/gramAdminClient";
import { errorMessage, type AdminOrganization } from "@/lib/gramAdminApi";
import type { DirectoryHandoff } from "@gram/admin-client/models/components/directoryhandoff";
import type { WorkosEnvironment } from "@gram/admin-client/models/components/directoryhandoffresult";
import { fmtDateShort } from "@/lib/utils";
import { useWriteReport } from "@/pages/organizations/writeReport";
import type { WriteReporter } from "@/pages/organizations/OrganizationActions";

// The organizations list, not a deep link to this organization. Every WorkOS
// dashboard address embeds an environment id, and nothing this app can read
// carries ours: the organization record holds a WorkOS organization id and
// nothing about the environment it lives in. A guessed environment id sends the
// operator to another tenant's dashboard or to a 404, so the link stops at the
// list and the copy below says which environment to be in.
const WORKOS_ORGANIZATIONS_URL = "https://dashboard.workos.com/organizations";

// What the operator does in WorkOS before there is anything to store here. One
// line each, in the order the dashboard asks for them.
const WORKOS_STEPS = [
  "Verify this organization's domain.",
  "Create a directory for it. WorkOS mints the SCIM endpoint and the bearer token as the directory is created.",
  "Copy both from that screen and paste them below. WorkOS shows the token once.",
];

function environmentNote(environment: WorkosEnvironment): string {
  switch (environment) {
    case "development":
      return "This server talks to the WorkOS Development environment.";
    case "production":
      return "This server talks to the WorkOS Production environment.";
    case "unknown":
      // Said rather than guessed. An operator told the wrong environment
      // creates the directory in it, and the endpoint stored here then belongs
      // to a directory this server never reads.
      return "This server does not report which WorkOS environment its key belongs to.";
  }
}

/** WorkOS states are lower snake case; they read as words in a record. */
function readable(state: string): string {
  return state.replace(/_/g, " ");
}

function endpointProblem(value: string): string | undefined {
  const trimmed = value.trim();
  if (!trimmed)
    return "Paste the SCIM endpoint WorkOS shows for the directory.";
  let parsed: URL;
  try {
    parsed = new URL(trimmed);
  } catch {
    return "That is not a web address. Paste the endpoint exactly as WorkOS shows it.";
  }
  if (parsed.protocol !== "https:") {
    return "The endpoint has to be https.";
  }
  return undefined;
}

// Named for what it stores, not "Save": this panel sits among several on the
// record, and a lone Save there says nothing about which of them it writes.
function saveLabel(isSaving: boolean, hasStoredHandoff: boolean): string {
  if (isSaving) return "Storing...";
  return hasStoredHandoff ? "Replace handoff" : "Store handoff";
}

function Detail({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <div className="grid gap-1 py-1 sm:grid-cols-[9rem_minmax(0,1fr)] sm:items-center sm:gap-3">
      <span className="text-muted-foreground text-sm">{label}</span>
      <div className="min-w-0 text-sm">{children}</div>
    </div>
  );
}

/**
 * What is stored, once something is. The token is not here and has no field on
 * the record: the fingerprint is how one stored token is told from another.
 */
function StoredHandoff({
  handoff,
}: {
  handoff: DirectoryHandoff;
}): JSX.Element {
  return (
    <div className="bg-muted/20 border p-3">
      <Detail label="Directory endpoint">
        <CopyValue
          label="Directory endpoint"
          value={handoff.scimBaseUrl}
          className="text-sm"
        />
      </Detail>
      <Detail label="Token fingerprint">
        <code className="font-mono text-xs">{handoff.tokenFingerprint}</code>
      </Detail>
      <Detail label="Stored by">
        {handoff.setBy} on {fmtDateShort(handoff.updatedAt.toISOString())}
      </Detail>
      <Detail label="WorkOS directory">
        {handoff.workosDirectoryState ? (
          <span className="flex flex-wrap items-center gap-2">
            <span>{readable(handoff.workosDirectoryState)}</span>
            {handoff.workosDirectoryId && (
              <CopyValue
                label="WorkOS directory ID"
                value={handoff.workosDirectoryId}
                className="text-sm"
              />
            )}
          </span>
        ) : (
          // The read asks WorkOS for the directory every time, so nothing back
          // means WorkOS has no directory at this endpoint rather than that the
          // state is merely unread.
          <span className="text-muted-foreground">
            Not found in WorkOS. The endpoint may belong to another environment.
          </span>
        )}
      </Detail>
    </div>
  );
}

/**
 * Phase one of the SCIM handoff: a platform operator creates the directory in
 * WorkOS and stores its endpoint and bearer token against the organization, so
 * the organization's own onboarding can configure Okta from stored values.
 *
 * The token is write-only. It is typed into a password field that is never
 * prefilled, sent once, cleared on success, and never read back: nothing on
 * this page can show a stored token, because nothing it reads carries one.
 *
 * Rendered in two places, so it draws its own body and no panel: the
 * organization record wraps it in a panel, and the create dialog shows it as
 * the step after the organization lands.
 */
export function DirectoryHandoffSection({
  org,
  reporter,
}: {
  org: AdminOrganization;
  /**
   * The create dialog is drawn above the table, outside the provider the record
   * pages sit in, so it hands its own reporter down the way it does to every
   * other write it owns.
   */
  reporter?: WriteReporter;
}): JSX.Element {
  const qc = useQueryClient();
  const [confirm, confirmDialog] = useConfirmDialog();
  const contextReporter = useWriteReport();
  const { announce, showFailure } = reporter ?? contextReporter;
  const endpointField = useId();
  const tokenField = useId();
  const messageID = useId();

  const [endpoint, setEndpoint] = useState("");
  const [token, setToken] = useState("");
  const [problem, setProblem] = useState<string | null>(null);
  const saveControl = useRef<HTMLButtonElement>(null);
  const clearControl = useRef<HTMLButtonElement>(null);
  const mounted = useRef(true);
  useOnUnmount(() => {
    mounted.current = false;
  });

  const handoffQuery = useQuery(organizationDirectoryHandoffQuery(org.id));
  const handoff = handoffQuery.data?.handoff ?? null;
  const environment = handoffQuery.data?.workosEnvironment ?? "unknown";

  const save = useMutation({
    mutationFn: setOrganizationDirectoryHandoff,
  });
  const clear = useMutation({
    mutationFn: () =>
      clearOrganizationDirectoryHandoff({ organizationId: org.id }),
  });
  const busy = save.isPending || clear.isPending;

  // Where the keyboard goes when a confirmation closes. `useConfirmDialog` has
  // no trigger to restore through, so Radix leaves focus on `document.body`.
  const restoreFocus = (control: React.RefObject<HTMLButtonElement | null>) => {
    setTimeout(() => {
      const target = control.current;
      if (mounted.current && target?.isConnected && !target.disabled) {
        target.focus();
      }
    });
  };

  const submit = async (): Promise<void> => {
    const endpointFault = endpointProblem(endpoint);
    if (endpointFault) {
      setProblem(endpointFault);
      return;
    }
    if (!token.trim()) {
      setProblem(
        "Paste the bearer token WorkOS showed when it created the directory.",
      );
      return;
    }
    setProblem(null);

    const confirmed = await confirm({
      title: handoff
        ? `Replace the directory handoff for ${org.name}?`
        : `Store the directory handoff for ${org.name}?`,
      description: handoff
        ? "The stored endpoint and token are replaced. Okta keeps whatever it was configured with until this organization's administrator runs the directory step again."
        : "This organization's administrator can then set up directory sync from their onboarding without a portal round trip.",
      details: (
        <dl className="bg-muted/20 grid gap-2 border p-3 text-sm">
          <div className="grid gap-1 sm:grid-cols-[8rem_minmax(0,1fr)]">
            <dt className="text-muted-foreground">Organization</dt>
            <dd className="break-all font-mono">
              {org.name} ({org.id})
            </dd>
          </div>
          <div className="grid gap-1 sm:grid-cols-[8rem_minmax(0,1fr)]">
            <dt className="text-muted-foreground">Endpoint</dt>
            {/* The endpoint, and nothing about the token: a confirmation is
                read aloud, screenshotted and pasted into tickets. */}
            <dd className="break-all font-mono">{endpoint.trim()}</dd>
          </div>
        </dl>
      ),
      confirmLabel: handoff ? "Replace handoff" : "Store handoff",
    });
    if (!mounted.current) return;
    if (!confirmed) {
      restoreFocus(saveControl);
      return;
    }

    showFailure(null);
    try {
      await save.mutateAsync({
        organizationId: org.id,
        scimBaseUrl: endpoint.trim(),
        scimToken: token.trim(),
      });
      if (!mounted.current) return;
      // Both fields, and the token first in intent: it has been sent, so the
      // only copy of it left in this tab is the one in this input.
      setToken("");
      setEndpoint("");
      invalidateOrganizationDirectoryHandoff(qc, org.id);
      announce(`Stored the directory handoff for ${org.name}.`);
    } catch (error) {
      if (!mounted.current) return;
      const text = `Could not store the directory handoff for ${org.name}: ${errorMessage(error)}`;
      announce(text);
      showFailure(text);
    } finally {
      restoreFocus(saveControl);
    }
  };

  const remove = async (): Promise<void> => {
    const confirmed = await confirm({
      title: `Clear the directory handoff for ${org.name}?`,
      description:
        "The stored endpoint and token are deleted. This organization's directory step goes back to the portal round trip, and anything already configured in Okta keeps running.",
      confirmLabel: "Clear handoff",
      destructive: true,
    });
    if (!mounted.current) return;
    if (!confirmed) {
      restoreFocus(clearControl);
      return;
    }

    showFailure(null);
    try {
      await clear.mutateAsync();
      if (!mounted.current) return;
      invalidateOrganizationDirectoryHandoff(qc, org.id);
      announce(`Cleared the directory handoff for ${org.name}.`);
    } catch (error) {
      if (!mounted.current) return;
      const text = `Could not clear the directory handoff for ${org.name}: ${errorMessage(error)}`;
      announce(text);
      showFailure(text);
    } finally {
      restoreFocus(clearControl);
    }
  };

  return (
    <div className="space-y-5">
      <div className="space-y-2">
        <p className="text-muted-foreground text-sm">
          {environmentNote(environment)} The link opens whichever environment
          you were last signed in to, so check the one you land in before
          creating anything.
        </p>
        <div className="flex flex-wrap items-center gap-3">
          <a
            href={WORKOS_ORGANIZATIONS_URL}
            target="_blank"
            rel="noopener noreferrer"
            className="text-sm underline underline-offset-4"
          >
            Open WorkOS organizations
          </a>
          {org.workos_id ? (
            <span className="flex items-center gap-1.5">
              <span className="text-muted-foreground text-sm">Search for</span>
              <CopyValue
                label="WorkOS organization ID"
                value={org.workos_id}
                className="text-sm"
              />
            </span>
          ) : (
            <span className="text-muted-foreground text-sm">
              This organization has no WorkOS organization yet.
            </span>
          )}
        </div>
      </div>

      <ol className="space-y-2">
        {WORKOS_STEPS.map((step, position) => (
          <li key={step} className="flex gap-3">
            <span
              aria-hidden="true"
              className="text-muted-foreground flex h-6 w-6 flex-shrink-0 items-center justify-center border text-xs font-semibold"
            >
              {position + 1}
            </span>
            <span className="min-w-0 flex-1 text-sm">{step}</span>
          </li>
        ))}
      </ol>

      <div className="grid gap-4">
        <div className="grid gap-2">
          <label htmlFor={endpointField} className="text-sm font-medium">
            Directory endpoint
          </label>
          <Input
            id={endpointField}
            value={endpoint}
            autoComplete="off"
            placeholder="https://api.workos.com/scim/v2/..."
            disabled={busy}
            aria-invalid={Boolean(problem)}
            aria-describedby={problem ? messageID : undefined}
            onChange={(event) => {
              setEndpoint(event.target.value);
              setProblem(null);
            }}
          />
        </div>
        <div className="grid gap-2">
          <label htmlFor={tokenField} className="text-sm font-medium">
            Bearer token
          </label>
          {/* Never prefilled, and there is nothing to prefill it from: no read
              on this page answers with a token. Replacing a handoff means
              pasting the token again, which is the point. */}
          <Input
            id={tokenField}
            type="password"
            value={token}
            autoComplete="off"
            disabled={busy}
            aria-describedby={problem ? messageID : undefined}
            onChange={(event) => {
              setToken(event.target.value);
              setProblem(null);
            }}
          />
          <p className="text-muted-foreground text-xs">
            Sent once and stored encrypted. It is never shown again here or
            anywhere else in this app.
          </p>
        </div>
        {problem && (
          <p id={messageID} role="alert" className="text-destructive text-sm">
            {problem}
          </p>
        )}
        <div>
          <Button
            ref={saveControl}
            // Explicit, because this section renders inside the create dialog's
            // markup too: a button with no type submits whatever form encloses
            // it, and creating an organization is not what Save means here.
            type="button"
            size="sm"
            disabled={busy}
            onClick={() => void submit()}
          >
            {saveLabel(save.isPending, handoff !== null)}
          </Button>
        </div>
      </div>

      {handoffQuery.isPending && (
        <p className="text-muted-foreground text-sm">Loading...</p>
      )}
      {handoffQuery.isError && (
        <p className="text-destructive text-sm">
          Could not read the stored handoff: {errorMessage(handoffQuery.error)}
        </p>
      )}
      {handoff && (
        <div className="space-y-3">
          <StoredHandoff handoff={handoff} />
          <Button
            ref={clearControl}
            type="button"
            variant="outline"
            size="sm"
            disabled={busy}
            onClick={() => void remove()}
          >
            {clear.isPending ? "Clearing..." : "Clear handoff"}
          </Button>
        </div>
      )}

      {confirmDialog}
    </div>
  );
}
