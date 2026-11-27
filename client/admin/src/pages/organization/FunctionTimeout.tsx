import { useId, useState, type FormEvent, type JSX } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  cancelOrganizationFetches,
  invalidateOrganizationDetails,
  invalidateOrganizationStats,
  invalidateOrganizations,
  writeOrganizationToCache,
} from "@/lib/adminQueries";
import {
  errorMessage,
  GramAdminError,
  setFunctionTimeout,
  type AdminOrganization,
} from "@/lib/gramAdminApi";
import {
  DEFAULT_FUNCTION_TIMEOUT_SECONDS,
  formatFunctionTimeout,
  MAX_FUNCTION_TIMEOUT_SECONDS,
  MIN_FUNCTION_TIMEOUT_SECONDS,
  parseFunctionTimeout,
} from "@/pages/organization/functionTimeout";
import { useWriteReport } from "@/pages/organizations/writeReport";

const LABEL = "Function timeout in seconds";

export function FunctionTimeout({
  org,
}: {
  org: AdminOrganization;
}): JSX.Element {
  const qc = useQueryClient();
  const { announce } = useWriteReport();
  const messageID = useId();
  const seconds =
    org.function_timeout_seconds ?? DEFAULT_FUNCTION_TIMEOUT_SECONDS;
  const isDefault = org.function_timeout_is_default ?? true;
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(String(seconds));
  // A problem with the value itself marks the field invalid; a failed save
  // does not, because the value may be fine and only needs a retry.
  const [problem, setProblem] = useState<string | null>(null);
  const [saveError, setSaveError] = useState<string | null>(null);

  const mutation = useMutation({
    mutationFn: setFunctionTimeout,
    onMutate: () => cancelOrganizationFetches(qc),
    onSuccess: (updated) => {
      writeOrganizationToCache(qc, updated);
      // The timeout is not on list rows or in the stats, so only the detail
      // record needs to be read again.
      invalidateOrganizationDetails(qc, updated);
      announce(
        updated.function_timeout_is_default
          ? "Function timeout reset to the default"
          : "Function timeout saved",
      );
      setEditing(false);
    },
    onError: (error) => {
      // onMutate cancelled in-flight organization reads; restart them. When
      // the write may have landed anyway, refetch the records so a server-side
      // success is not hidden behind the stale cached value.
      invalidateOrganizationStats(qc);
      if (
        !(error instanceof GramAdminError) ||
        error.status === 404 ||
        error.status === 409 ||
        error.status >= 500
      ) {
        void invalidateOrganizations(qc);
      }
      const text = `Couldn't save the function timeout: ${errorMessage(error)}`;
      announce(text);
      setSaveError(text);
    },
  });

  const startEditing = () => {
    setDraft(String(seconds));
    setProblem(null);
    setSaveError(null);
    setEditing(true);
  };

  const onSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const parsed = parseFunctionTimeout(draft);
    setProblem(parsed.problem);
    setSaveError(null);
    if (parsed.seconds === null) {
      return;
    }
    mutation.mutate({
      organization_id: org.id,
      function_timeout_seconds: parsed.seconds,
    });
  };

  const resetToDefault = () => {
    setProblem(null);
    setSaveError(null);
    mutation.mutate({ organization_id: org.id });
  };

  if (editing) {
    return (
      <form
        noValidate
        onSubmit={onSubmit}
        className="flex flex-col gap-1.5"
        aria-label="Function Timeout"
      >
        <div className="flex flex-wrap items-center gap-2">
          <Input
            type="number"
            inputMode="numeric"
            min={MIN_FUNCTION_TIMEOUT_SECONDS}
            max={MAX_FUNCTION_TIMEOUT_SECONDS}
            step={1}
            autoFocus
            value={draft}
            aria-label={LABEL}
            aria-invalid={problem ? true : undefined}
            aria-describedby={messageID}
            disabled={mutation.isPending}
            onChange={(event) => setDraft(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Escape") {
                setEditing(false);
              }
            }}
            className="h-8 w-24 text-sm tabular-nums"
          />
          <span className="text-muted-foreground text-sm">seconds</span>
          <Button type="submit" size="sm" disabled={mutation.isPending}>
            {mutation.isPending ? "Saving…" : "Save"}
          </Button>
          {isDefault ? null : (
            <Button
              type="button"
              size="sm"
              variant="outline"
              disabled={mutation.isPending}
              onClick={resetToDefault}
            >
              Use default
            </Button>
          )}
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
        {problem || saveError ? (
          <p id={messageID} className="text-destructive text-xs">
            {problem ?? saveError}
          </p>
        ) : (
          <p id={messageID} className="text-muted-foreground text-xs">
            How long one function call may run, from{" "}
            {MIN_FUNCTION_TIMEOUT_SECONDS} to {MAX_FUNCTION_TIMEOUT_SECONDS}{" "}
            seconds. The default is {DEFAULT_FUNCTION_TIMEOUT_SECONDS}.
          </p>
        )}
      </form>
    );
  }

  return (
    <div className="flex min-w-0 flex-wrap items-center gap-2">
      <span className="text-sm tabular-nums">
        {formatFunctionTimeout(seconds)}
      </span>
      {isDefault ? (
        <span className="text-muted-foreground text-sm">(default)</span>
      ) : null}
      <Button
        type="button"
        size="xs"
        variant="outline"
        aria-label="Edit function timeout"
        onClick={startEditing}
      >
        Edit
      </Button>
    </div>
  );
}
