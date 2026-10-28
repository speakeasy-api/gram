import { useId, useState, type FormEvent, type JSX } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ExternalLinkIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  cancelOrganizationFetches,
  invalidateOrganizations,
  writeOrganizationToCache,
} from "@/lib/adminQueries";
import {
  errorMessage,
  setAdoptionDashboardUrl,
  type AdminOrganization,
} from "@/lib/gramAdminApi";
import { adoptionDashboardUrlProblem } from "@/pages/organization/adoptionDashboardUrl";
import { useWriteReport } from "@/pages/organizations/writeReport";

export function AdoptionDashboardLink({
  org,
}: {
  org: AdminOrganization;
}): JSX.Element {
  const qc = useQueryClient();
  const { announce, showFailure } = useWriteReport();
  const inputID = useId();
  const messageID = useId();
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(org.adoption_dashboard_url ?? "");
  const [problem, setProblem] = useState<string | null>(null);

  const mutation = useMutation({
    mutationFn: setAdoptionDashboardUrl,
    onMutate: () => cancelOrganizationFetches(qc),
    onSuccess: (updated) => {
      writeOrganizationToCache(qc, updated);
      void invalidateOrganizations(qc);
      showFailure(null);
      announce(
        updated.adoption_dashboard_url
          ? "Adoption dashboard link saved"
          : "Adoption dashboard link removed",
      );
      setEditing(false);
    },
    onError: (error) => {
      const text = `Couldn't save the adoption dashboard link: ${errorMessage(error)}`;
      announce(text);
      setProblem(text);
    },
  });

  const startEditing = () => {
    setDraft(org.adoption_dashboard_url ?? "");
    setProblem(null);
    setEditing(true);
  };

  const onSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const found = adoptionDashboardUrlProblem(draft);
    setProblem(found);
    if (found) {
      return;
    }
    mutation.mutate({
      organization_id: org.id,
      adoption_dashboard_url: draft.trim(),
    });
  };

  if (editing) {
    return (
      <form
        onSubmit={onSubmit}
        className="flex flex-col gap-1.5"
        aria-label="Adoption Dashboard Link"
      >
        <div className="flex flex-wrap items-center gap-2">
          <Input
            id={inputID}
            type="url"
            inputMode="url"
            autoFocus
            value={draft}
            placeholder="https://claude.ai/artifact/…"
            aria-label="Adoption Dashboard Link"
            aria-invalid={problem ? true : undefined}
            aria-describedby={problem ? messageID : undefined}
            disabled={mutation.isPending}
            onChange={(event) => setDraft(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Escape") {
                setEditing(false);
              }
            }}
            className="h-8 min-w-0 flex-1 text-sm"
          />
          <Button type="submit" size="sm" disabled={mutation.isPending}>
            {mutation.isPending ? "Saving…" : "Save"}
          </Button>
          <Button
            type="button"
            size="sm"
            variant="ghost"
            disabled={mutation.isPending}
            onClick={() => setEditing(false)}
          >
            Cancel
          </Button>
        </div>
        {problem ? (
          <p id={messageID} className="text-destructive text-xs">
            {problem}
          </p>
        ) : (
          <p className="text-muted-foreground text-xs">
            Leave empty and save to remove the link.
          </p>
        )}
      </form>
    );
  }

  return (
    <div className="flex min-w-0 flex-wrap items-center gap-2">
      {org.adoption_dashboard_url ? (
        <a
          href={org.adoption_dashboard_url}
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex min-w-0 items-center gap-1 text-sm underline-offset-2 hover:underline"
        >
          <span className="truncate">{org.adoption_dashboard_url}</span>
          <ExternalLinkIcon className="size-3.5 shrink-0" aria-hidden="true" />
        </a>
      ) : (
        <span className="text-muted-foreground text-sm">-</span>
      )}
      <Button type="button" size="xs" variant="outline" onClick={startEditing}>
        {org.adoption_dashboard_url ? "Edit" : "Add link"}
      </Button>
    </div>
  );
}
