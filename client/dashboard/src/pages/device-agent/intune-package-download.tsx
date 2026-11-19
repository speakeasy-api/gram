import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import { useAgentToken } from "@/hooks/useAgentToken";
import { downloadBlob } from "@/lib/download";
import { useOrgRoutes } from "@/routes";
import { useRevokeAPIKeyMutation } from "@gram/client/react-query/revokeAPIKey";
import { Download } from "lucide-react";
import React, { useState } from "react";
import { Link } from "react-router";
import { buildIntuneBundle, configRevision } from "./intune-package";

type Status =
  | { kind: "idle" }
  | { kind: "working" }
  | { kind: "done"; fileName: string }
  | { kind: "error"; message: string };

const LINK_CLASS = "underline underline-offset-2 hover:text-foreground";

// IntunePackageDownload builds the ready-to-upload Win32 apps for Intune in
// the browser: it fetches the signed msi, mints an agent token for the
// config app, and saves the bundle. Nothing about the bundle reaches the
// server beyond the token mint.
export function IntunePackageDownload({
  version,
  msiUrl,
  autoUpdate,
}: {
  version: string | null;
  msiUrl: string | null;
  autoUpdate: "automatic" | "notify";
}): React.JSX.Element {
  const { name: orgName, slug: orgSlug } = useOrganization();
  const apiKeysHref = useOrgRoutes().apiKeys.href();
  const { canGenerate, generateAsync } = useAgentToken({});
  const revokeKey = useRevokeAPIKeyMutation();
  const [status, setStatus] = useState<Status>({ kind: "idle" });

  const ready = version !== null && msiUrl !== null;

  const download = async () => {
    if (!version || !msiUrl) return;
    setStatus({ kind: "working" });
    try {
      // Fetch first so a failed download doesn't leave an unused token behind.
      const res = await fetch(msiUrl);
      if (!res.ok)
        throw new Error(`Couldn't download the msi (HTTP ${res.status}).`);
      const msi = new Uint8Array(await res.arrayBuffer());

      const minted = await generateAsync();
      const fileName = `speakeasy-intune-${version}.zip`;
      try {
        const bundle = await buildIntuneBundle({
          orgToken: minted.token,
          orgSlug,
          orgName,
          autoUpdate,
          revision: configRevision(new Date()),
          version,
          msi,
        });
        downloadBlob(
          new Blob([bundle as BlobPart], { type: "application/zip" }),
          fileName,
        );
      } catch (err) {
        // Best-effort: don't leave a token behind for a bundle nobody got.
        await revokeKey
          .mutateAsync({
            security: { sessionHeaderGramSession: "" },
            request: { id: minted.id },
          })
          .catch(() => undefined);
        throw err;
      }
      setStatus({ kind: "done", fileName });
    } catch (err) {
      setStatus({
        kind: "error",
        message: err instanceof Error ? err.message : String(err),
      });
    }
  };

  return (
    <div className="bg-secondary/30 border-border flex flex-col gap-3 rounded-md border p-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-col gap-1">
          <Text className="font-medium">Microsoft Intune</Text>
          <Text small muted>
            Download the three Win32 apps ready to upload, already set up for
            this organization. You don't need IntuneWinAppUtil. The README
            inside lists the install commands and detection rules for each app.
          </Text>
        </div>
        <Button
          variant="secondary"
          onClick={() => void download()}
          disabled={!ready || !canGenerate || status.kind === "working"}
          tooltip={
            !canGenerate
              ? "Building the Intune package mints an agent token, which requires the org:admin role."
              : !ready
                ? "Looking up the latest agent version…"
                : undefined
          }
        >
          <Button.LeftIcon>
            <Download className="h-4 w-4" />
          </Button.LeftIcon>
          <Button.Text>
            {status.kind === "working"
              ? "Building package…"
              : "Download Intune package"}
          </Button.Text>
        </Button>
      </div>

      {status.kind === "done" && (
        <Alert variant="warning">
          <AlertTitle>{status.fileName} downloaded</AlertTitle>
          <AlertDescription>
            The config app inside has a new agent token, so handle the bundle
            like an API key. Existing tokens keep working. Manage or revoke them
            under Settings →{" "}
            <Link to={apiKeysHref} className={LINK_CLASS}>
              API Keys
            </Link>
            .
          </AlertDescription>
        </Alert>
      )}

      {status.kind === "error" && (
        <Alert variant="error">
          <AlertTitle>Couldn't build the Intune package</AlertTitle>
          <AlertDescription>{status.message}</AlertDescription>
        </Alert>
      )}
    </div>
  );
}
