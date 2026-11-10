import { useState, type JSX } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import type { AdminMcpDebugReport } from "@gram/admin-client/models/components/adminmcpdebugreport";
import { adminDebugMcpUrl, registryEntryQuery } from "@/lib/gramAdminClient";
import { registryEntryName } from "@/lib/registryEntryName";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { McpDebugger } from "./McpDebugger";
import { VerifyInSpeakeasy } from "./VerifyInSpeakeasy";
import { registryRemotes, urlPlaceholders } from "./registryRemotes";

function isHttpUrl(value: string): boolean {
  try {
    const url = new URL(value.trim());
    return url.protocol === "https:" || url.protocol === "http:";
  } catch {
    return false;
  }
}

/**
 * The staff MCP debugger. Opened from a registry entry it starts on that
 * entry's remote URL; opened on its own it takes any URL.
 */
export function RegistryDebugPage(): JSX.Element {
  const search = useSearch({ from: "/registry/debug" });
  const navigate = useNavigate();
  const entry = useQuery({
    ...registryEntryQuery(search.entry ?? ""),
    enabled: !!search.entry,
  });
  const remotes = entry.data ? registryRemotes(entry.data.dataJson) : [];
  const entryName = entry.data
    ? (registryEntryName(entry.data.dataJson) ?? "Unnamed entry")
    : undefined;

  // The typed URL wins; until the entry loads or something is typed, start
  // from the address, then from the entry's first remote.
  const [draft, setDraft] = useState<string | undefined>(search.url);
  const url = draft ?? remotes[0]?.url ?? "";
  const placeholders = urlPlaceholders(url);

  const debug = useMutation<AdminMcpDebugReport, Error, string>({
    mutationFn: (target) => adminDebugMcpUrl(target),
  });

  // Verifying needs a server that answered: an unreachable or non-MCP URL has
  // nothing to sign in to.
  const report = debug.isPending ? undefined : debug.data;
  const verifiable =
    report?.outcome === "mcp_available" ||
    report?.outcome === "authentication_required";

  const run = () => {
    const target = url.trim();
    debug.mutate(target);
    void navigate({
      to: "/registry/debug",
      search: (previous) => ({ ...previous, url: target }),
      replace: true,
    });
  };

  return (
    <div className="space-y-6 p-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="space-y-1">
          <h1 className="text-2xl font-semibold">
            {entryName ? `Debug ${entryName}` : "MCP debugger"}
          </h1>
          <p className="text-muted-foreground text-sm">
            Trace how a client reaches an MCP server, how it asks for sign-in,
            and what it offers.
          </p>
        </div>
        {search.entry ? (
          <Button variant="outline" asChild>
            <Link to="/registry/$entryId" params={{ entryId: search.entry }}>
              Open entry
            </Link>
          </Button>
        ) : null}
      </div>

      {search.entry && entry.error ? (
        <div role="alert" className="text-destructive text-sm">
          Couldn't load the registry entry: {entry.error.message}
        </div>
      ) : null}

      <McpDebugger
        url={url}
        onUrlChange={setDraft}
        onRun={run}
        canRun={isHttpUrl(url) && placeholders.length === 0}
        isRunning={debug.isPending}
        report={report}
        verifyAction={
          report && verifiable ? (
            <VerifyInSpeakeasy url={report.url} name={report.serverName} />
          ) : undefined
        }
        error={debug.isPending ? undefined : debug.error?.message}
        urlHint={
          remotes.length > 1 || placeholders.length > 0 ? (
            <div className="flex flex-wrap items-center gap-3 border-b px-4 py-3 text-sm">
              {remotes.length > 1 ? (
                <Select
                  value={
                    remotes.some((remote) => remote.url === url) ? url : ""
                  }
                  onValueChange={(value) => setDraft(value)}
                >
                  <SelectTrigger aria-label="Remote" className="max-w-full">
                    <SelectValue placeholder="Pick one of the entry's remotes" />
                  </SelectTrigger>
                  <SelectContent>
                    {remotes.map((remote) => (
                      <SelectItem key={remote.url} value={remote.url}>
                        {remote.type ? `${remote.type} · ` : ""}
                        {remote.url}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              ) : null}
              {placeholders.length > 0 ? (
                <span className="text-muted-foreground">
                  Replace{" "}
                  {placeholders.map((name, index) => (
                    <span key={name}>
                      {index > 0 ? ", " : ""}
                      <code className="font-mono">{`{${name}}`}</code>
                    </span>
                  ))}{" "}
                  in the URL with a real value to run the trace.
                </span>
              ) : null}
            </div>
          ) : null
        }
      />

      {search.entry && entry.data && remotes.length === 0 ? (
        <p className="text-muted-foreground text-sm">
          This entry lists no remote URL. Enter one above to trace it.
        </p>
      ) : null}
    </div>
  );
}
