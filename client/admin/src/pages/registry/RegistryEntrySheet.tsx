import { useRef, useState, type JSX } from "react";
import { useBlocker } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { AdminRegistryEntry } from "@gram/admin-client/models/components/adminregistryentry";
import { Button } from "@/components/ui/button";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from "@/components/ui/sheet";
import {
  registryEntryQuery,
  useCreateRegistryEntryMutation,
  useSaveRegistryEntryMutation,
  useSetRegistryEntryPublishedMutation,
} from "@/lib/gramAdminClient";
import {
  type ValidationIssue,
  validateRegistryText,
} from "@/lib/registryValidation";

export const STAGE_A_NOTICE =
  "Edits affect the Gram catalog. Customer catalog reads still use Pulse.";
const EMPTY =
  '{\n  "server": {\n    "name": "",\n    "description": "",\n    "version": "1.0.0",\n    "remotes": []\n  }\n}';
type Props = {
  id: string | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
};

function errorStatus(error: unknown): number | undefined {
  return error &&
    typeof error === "object" &&
    "statusCode" in error &&
    typeof error.statusCode === "number"
    ? error.statusCode
    : undefined;
}

function errorText(error: unknown): string {
  return error instanceof Error
    ? error.message
    : "Request failed. Your text has been retained.";
}

// Mount a fresh editor per opening; background query updates never initialize it again.
export function RegistryEntrySheet(props: Props): JSX.Element | null {
  return props.open ? <Editor key={props.id ?? "new"} {...props} /> : null;
}

function Editor({ id, open, onOpenChange }: Props): JSX.Element {
  const queryClient = useQueryClient();
  const [base, setBase] = useState<AdminRegistryEntry | null>(null);
  const detail = useQuery(registryEntryQuery(base?.id ?? id ?? ""));
  const create = useCreateRegistryEntryMutation();
  const save = useSaveRegistryEntryMutation();
  const visibility = useSetRegistryEntryPublishedMutation();
  const [text, setText] = useState(id ? "" : EMPTY);
  const [failure, setFailure] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const [conflict, setConflict] = useState(false);
  const inFlight = useRef(false);
  const opener = useRef(document.activeElement as HTMLElement | null);
  if (id && !base && detail.data) {
    setBase(detail.data);
    setText(detail.data.dataJson);
  }

  const loaded = id === null || base !== null;
  const dirty = loaded && text !== (base?.dataJson ?? EMPTY);
  useBlocker({
    shouldBlockFn: () =>
      busy || (dirty && !window.confirm("Discard unsaved registry edits?")),
    enableBeforeUnload: dirty || busy,
  });
  const [issues, setIssues] = useState<ValidationIssue[]>([]);
  const close = (): void => {
    if (
      !inFlight.current &&
      (!dirty || window.confirm("Discard unsaved registry edits?"))
    )
      onOpenChange(false);
  };

  const accept = async (entry: AdminRegistryEntry): Promise<void> => {
    await queryClient.cancelQueries({
      queryKey: registryEntryQuery(entry.id).queryKey,
    });
    queryClient.setQueryData(registryEntryQuery(entry.id).queryKey, entry);
    setBase(entry);
    setText(entry.dataJson);
    setIssues([]);
    setFailure(null);
    setConflict(false);
    await queryClient.invalidateQueries({
      predicate: (query) => query.queryKey.includes("listRegistryEntries"),
    });
  };

  const perform = async (
    kind: "save" | "visibility" | "reload",
  ): Promise<void> => {
    if (inFlight.current) return;
    if (kind === "save") {
      const checked = validateRegistryText(text, base ?? undefined);
      setIssues(checked);
      if (checked.length > 0) return;
    }
    if (
      kind === "reload" &&
      !window.confirm("Reload the current entry and discard your edits?")
    )
      return;

    inFlight.current = true;
    setBusy(true);
    setFailure(null);

    try {
      if (kind === "reload") {
        const result = await detail.refetch();
        if (result.error) throw result.error;
        if (result.data) {
          setBase(result.data);
          setText(result.data.dataJson);
          setIssues([]);
          setConflict(false);
        }
      } else if (kind === "visibility" && base && !dirty) {
        await accept(
          await visibility.mutateAsync({
            request: {
              id: base.id,
              updatedAt: base.updatedAt,
              published: !base.published,
            },
          }),
        );
      } else if (kind === "save" && loaded && !conflict) {
        await accept(
          base
            ? await save.mutateAsync({
                request: {
                  id: base.id,
                  dataJson: text,
                  updatedAt: base.updatedAt,
                },
              })
            : await create.mutateAsync({ request: { dataJson: text } }),
        );
      }
    } catch (error) {
      setFailure(error);
      if (errorStatus(error) === 409) setConflict(true);
    } finally {
      inFlight.current = false;
      setBusy(false);
    }
  };

  return (
    <Sheet
      open={open}
      onOpenChange={(value) => {
        if (!value) close();
      }}
    >
      <SheetContent
        className="w-full overflow-y-auto sm:max-w-3xl"
        onCloseAutoFocus={(event) => {
          event.preventDefault();
          opener.current?.focus();
        }}
      >
        <SheetHeader>
          <SheetTitle>
            {base || id ? "Edit registry entry" : "New registry entry"}
          </SheetTitle>
          <SheetDescription>{STAGE_A_NOTICE}</SheetDescription>
        </SheetHeader>
        <div className="space-y-4 px-4 pb-6">
          <p className="text-muted-foreground text-sm">
            Stage A: existing server names and ordered endpoint structure are
            immutable. Do not add, remove, reorder, or change remote transports
            or URLs. Metadata edits remain available. Create publishes; Save
            preserves publication status.
          </p>
          {detail.error && !base && id && (
            <p role="alert">{errorText(detail.error)}</p>
          )}
          {failure !== null && (
            <div id="registry-request-error" role="alert">
              {errorText(failure)}{" "}
              {conflict &&
                "This entry changed. Reload explicitly before retrying."}
            </div>
          )}
          {!loaded ? (
            <p>Loading entry…</p>
          ) : (
            <>
              <label
                htmlFor="registry-json"
                className="block text-sm font-medium"
              >
                Record JSON
              </label>
              <textarea
                id="registry-json"
                className="border-input min-h-96 w-full rounded-md border p-3 font-mono text-sm"
                value={text}
                onChange={(event) => {
                  setText(event.target.value);
                  setIssues([]);
                }}
                onBlur={() =>
                  setIssues(validateRegistryText(text, base ?? undefined))
                }
                disabled={busy}
                aria-invalid={issues.length > 0 || errorStatus(failure) === 422}
                aria-describedby={
                  failure !== null
                    ? "registry-feedback registry-request-error"
                    : "registry-feedback"
                }
              />
              <div id="registry-feedback" className="text-sm">
                <p>
                  JSON syntax and size are checked on blur and Save. The server
                  validates record fields when you save.
                </p>
                {issues.length > 0 && (
                  <ul role="alert">
                    {issues.map((issue, index) => (
                      <li key={index}>
                        {issue.path}: {issue.message}
                      </li>
                    ))}
                  </ul>
                )}
                {base?.issues.map((issue, index) => (
                  <p key={index}>
                    Stored record {issue.path}: {issue.message}
                  </p>
                ))}
              </div>
            </>
          )}
          <div className="flex flex-wrap gap-2">
            <Button
              disabled={!loaded || busy || conflict}
              onClick={() => void perform("save")}
            >
              Save
            </Button>
            <Button variant="outline" disabled={busy} onClick={close}>
              Cancel
            </Button>
            {conflict && (
              <Button
                variant="outline"
                disabled={busy}
                onClick={() => void perform("reload")}
              >
                Reload
              </Button>
            )}
            {base && (
              <Button
                variant="outline"
                disabled={dirty || busy || conflict}
                onClick={() => void perform("visibility")}
              >
                {base.published ? "Unpublish" : "Republish"}
              </Button>
            )}
          </div>
          {dirty && base && (
            <p className="text-muted-foreground text-sm">
              Save or cancel edits before changing visibility.
            </p>
          )}
        </div>
      </SheetContent>
    </Sheet>
  );
}
