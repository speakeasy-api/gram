import { lazy, Suspense, useRef, useState, type JSX } from "react";
import {
  registryEntryName,
  registryEntryNameKey,
} from "@/lib/registryEntryName";
import { RegistryOktaCandidates } from "./RegistryOktaCandidates";
import { addOinName } from "./registryOktaNames";
import { useBlocker } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { AdminRegistryEntry } from "@gram/admin-client/models/components/adminregistryentry";
import { Button } from "@/components/ui/button";
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

import {
  formatRegistryJson,
  serverValidationIssues,
} from "@/lib/registryJsonEditor";

const RegistryJsonEditor = lazy(() => import("./RegistryJsonEditor"));

export const CATALOG_NOTICE = "Edits affect the Speakeasy catalog.";
const EMPTY =
  '{\n  "server": {\n    "name": "",\n    "description": "",\n    "version": "1.0.0",\n    "remotes": []\n  }\n}';
type Props = {
  id: string | null;
  // Called once a discard is confirmed; leaving must not prompt again.
  onClose: () => void;
  onCreated?: (id: string) => void;
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

// Mount a fresh editor per entry (key by id); background query updates never
// initialize it again.
export function RegistryEntryEditor({
  id,
  onClose,
  onCreated,
}: Props): JSX.Element {
  const queryClient = useQueryClient();
  const [base, setBase] = useState<AdminRegistryEntry | null>(null);
  const detail = useQuery(registryEntryQuery(base?.id ?? id ?? ""));
  const create = useCreateRegistryEntryMutation();
  const save = useSaveRegistryEntryMutation();
  const visibility = useSetRegistryEntryPublishedMutation();
  const [baseline, setBaseline] = useState(EMPTY);
  const [text, setText] = useState(id ? "" : EMPTY);
  const [serverIssues, setServerIssues] = useState<ValidationIssue[]>([]);
  const [edited, setEdited] = useState(false);
  const [failure, setFailure] = useState<unknown>(null);
  const [requestBusy, setBusy] = useState(false);
  const busy = requestBusy;
  const [conflict, setConflict] = useState(false);
  const inFlight = useRef(false);
  if (id && !base && detail.data) {
    setBase(detail.data);
    const formatted =
      formatRegistryJson(detail.data.dataJson) ?? detail.data.dataJson;
    setBaseline(formatted);
    setText(formatted);
  }

  const loaded = id === null || base !== null;
  const dirty = loaded && text !== baseline;
  useBlocker({
    shouldBlockFn: () =>
      busy || (dirty && !window.confirm("Discard unsaved registry edits?")),
    enableBeforeUnload: dirty || busy,
  });
  const name = base ? registryEntryName(base.dataJson) : undefined;
  const [issues, setIssues] = useState<ValidationIssue[]>([]);
  const close = (): void => {
    if (
      !inFlight.current &&
      (!dirty || window.confirm("Discard unsaved registry edits?"))
    )
      onClose();
  };

  const accept = async (entry: AdminRegistryEntry): Promise<void> => {
    await queryClient.cancelQueries({
      queryKey: registryEntryQuery(entry.id).queryKey,
    });
    queryClient.setQueryData(registryEntryQuery(entry.id).queryKey, entry);
    const name = registryEntryName(entry.dataJson);
    if (name)
      queryClient.setQueryData(registryEntryNameKey(entry.id), { name });
    setBase(entry);
    const formatted = formatRegistryJson(entry.dataJson) ?? entry.dataJson;
    setBaseline(formatted);
    setText(formatted);
    setIssues([]);
    setServerIssues([]);
    setEdited(false);
    setFailure(null);
    setConflict(false);
    await queryClient.invalidateQueries({
      predicate: (query) =>
        query.queryKey.some(
          (part) =>
            typeof part === "string" &&
            (part.includes("RegistryEntries") || part.includes("RegistryOkta")),
        ),
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
    setServerIssues([]);
    let created: string | undefined;

    try {
      if (kind === "reload") {
        const result = await detail.refetch();
        if (result.error) throw result.error;
        if (result.data) {
          setBase(result.data);
          const formatted =
            formatRegistryJson(result.data.dataJson) ?? result.data.dataJson;
          setBaseline(formatted);
          setText(formatted);
          setIssues([]);
          setServerIssues([]);
          setEdited(false);
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
        const dataJson = text;
        const checked = validateRegistryText(dataJson, base ?? undefined);
        if (checked.length > 0) {
          setIssues(checked);
          return;
        }
        const entry = base
          ? await save.mutateAsync({
              request: {
                id: base.id,
                dataJson,
                updatedAt: base.updatedAt,
              },
            })
          : await create.mutateAsync({ request: { dataJson } });
        await accept(entry);
        if (!base) created = entry.id;
      }
    } catch (error) {
      setFailure(error);
      if (errorStatus(error) === 422)
        setServerIssues(serverValidationIssues(errorText(error), text));
      if (errorStatus(error) === 409) setConflict(true);
    } finally {
      inFlight.current = false;
      setBusy(false);
    }
    // The created entry is accepted and clean, so its page can take over.
    if (created) onCreated?.(created);
  };

  return (
    <div className="flex flex-1 flex-col">
      <div className="flex flex-1 flex-col gap-3 py-6">
        <header className="shrink-0 space-y-1">
          <h1 className="text-2xl font-semibold break-all">
            {name ??
              (base || id ? "Edit registry entry" : "New registry entry")}
          </h1>
          <p className="text-muted-foreground text-sm">{CATALOG_NOTICE}</p>
        </header>
        <p className="text-muted-foreground shrink-0 text-sm">
          Stage A: existing server names and ordered endpoint structure are
          immutable. Do not add, remove, reorder, or change remote transports or
          URLs. Metadata edits remain available. Create publishes; Save
          preserves publication status.
        </p>
        {failure !== null && (
          <div
            id="registry-request-error"
            role="alert"
            className="max-h-28 shrink-0 overflow-y-auto whitespace-pre-wrap text-sm"
          >
            {serverIssues.length > 0 ? (
              <ul>
                {serverIssues.map((issue, index) => (
                  <li key={index}>
                    {issue.path}: {issue.message}
                  </li>
                ))}
              </ul>
            ) : (
              errorText(failure)
            )}{" "}
            {conflict &&
              "This entry changed. Reload explicitly before retrying."}
          </div>
        )}
        {!loaded ? (
          detail.error ? (
            <div role="alert">
              <p>{errorText(detail.error)}</p>
              <Button
                variant="outline"
                disabled={detail.isFetching}
                onClick={() => void detail.refetch()}
              >
                Retry
              </Button>
            </div>
          ) : (
            <p>Loading entry…</p>
          )
        ) : (
          <>
            {base && (
              <RegistryOktaCandidates
                entryId={base.id}
                text={text}
                disabled={busy}
                onAdd={(name) => {
                  const next = addOinName(text, name);
                  if ("error" in next) {
                    setFailure(new Error(next.error));
                    return;
                  }
                  setText(next.text);
                  setIssues([]);
                  setServerIssues([]);
                  // A conflict keeps its explanation; Save stays disabled
                  // until the entry is reloaded.
                  if (!conflict) setFailure(null);
                  setEdited(true);
                }}
              />
            )}
            <Suspense fallback={<p role="status">Loading JSON editor…</p>}>
              <RegistryJsonEditor
                value={text}
                onChange={(value) => {
                  setText(value);
                  setIssues([]);
                  setServerIssues([]);
                  setEdited(true);
                  if (errorStatus(failure) === 422) setFailure(null);
                }}
                onBlur={() =>
                  setIssues(validateRegistryText(text, base ?? undefined))
                }
                disabled={busy}
                invalid={issues.length > 0 || serverIssues.length > 0}
                describedBy={
                  failure !== null
                    ? "registry-feedback registry-request-error"
                    : "registry-feedback"
                }
                issues={
                  serverIssues.length > 0
                    ? serverIssues
                    : !edited
                      ? (base?.issues ?? [])
                      : []
                }
              />
            </Suspense>
            <div
              id="registry-feedback"
              className="max-h-28 shrink-0 overflow-y-auto text-sm"
            >
              <p>
                Syntax and schema feedback appear while editing. Suggestions are
                optional; values are never applied automatically. Size is
                checked on blur and Save. Browser schema checks can differ from
                the server, which validates records when you save.
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
      </div>
      <div className="bg-background sticky bottom-0 shrink-0 space-y-2 border-t py-4">
        <div className="flex flex-wrap gap-2">
          <Button
            disabled={!loaded || busy || conflict || (base !== null && !dirty)}
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
          <p className="text-muted-foreground shrink-0 text-sm">
            Save or cancel edits before changing visibility.
          </p>
        )}
      </div>
    </div>
  );
}
