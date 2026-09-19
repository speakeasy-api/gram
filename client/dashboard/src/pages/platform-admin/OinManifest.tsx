import { Page } from "@/components/page-layout";
import { Alert } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { MetricCard } from "@/components/ui/MetricCard";
import { Skeleton } from "@/components/ui/Skeleton";
import { Table, type Column } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { useSdkClient } from "@/contexts/Sdk";
import { filenameFromDisposition } from "./contentDisposition";
import type { Gram } from "@gram/client";
import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import { useState } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { toast } from "sonner";
import { AdminSection } from "./AdminSection";
import { StrictPlatformAdminGate } from "./StrictPlatformAdminGate";

// Mirrors the server's oinmanifest.Manifest JSON shape (manifest_version 1).
// Only the fields the page reads are typed; the download hands over the raw
// body untouched so the file matches a curl of the same RPC byte for byte.
type OinManifest = {
  manifest_version: number;
  generated_at: string;
  requesting_app: {
    name: string;
    org_domain: string;
    sso_mode: string;
    role: string;
    redirect_uri: string;
  };
  resource_registrations: OinRegistration[];
  summary: { registrations: number; ready: number; blocked: number };
};

export type OinRegistration = {
  resource_name: string;
  resource_as_issuer: string;
  resource_identifier: string;
  xaa_audience: string | null;
  client_id: string | null;
  registration: string;
  scopes: string[];
  blockers: string[];
};

type ExportFormat = "json" | "markdown";

// filename is absent when the browser cannot read Content-Disposition (the
// server does not expose it across origins); the page then rebuilds the
// server's own naming from the manifest's generation date.
type ManifestExport = { body: string; filename?: string };

const EXTENSIONS: Record<ExportFormat, string> = {
  json: "json",
  markdown: "md",
};

function defaultFilename(format: ExportFormat, generatedAt: string): string {
  return `speakeasy-oin-xaa-manifest-${generatedAt.slice(0, 10)}.${EXTENSIONS[format]}`;
}

const CONTENT_TYPES: Record<ExportFormat, string> = {
  json: "application/json",
  markdown: "text/markdown;charset=utf-8",
};

// The SDK hands headers back as a plain Record keyed with the server's own
// casing, so the lookup is case-insensitive by hand. The body is kept
// untouched so the download is byte-identical to a curl of the same RPC.
function headerValue(
  headers: Record<string, string[]>,
  name: string,
): string | null {
  const key = Object.keys(headers).find(
    (k) => k.toLowerCase() === name.toLowerCase(),
  );
  return key ? (headers[key]?.[0] ?? null) : null;
}

async function fetchManifestExport(
  client: Gram,
  format: ExportFormat,
): Promise<ManifestExport> {
  const { headers, result } = await client.oinManifest.export({ format });
  return {
    body: await new Response(result).text(),
    filename: filenameFromDisposition(
      headerValue(headers, "Content-Disposition"),
    ),
  };
}

function useManifestExport(
  format: ExportFormat,
): UseQueryResult<ManifestExport, Error> {
  const client = useSdkClient();
  return useQuery({
    queryKey: ["platform-admin", "oin-manifest", format],
    queryFn: () => fetchManifestExport(client, format),
    // The export is a platform-admin action that leaves an audit line per
    // call; refetching on focus would spam it for no new information.
    refetchOnWindowFocus: false,
    retry: false,
    throwOnError: false,
  });
}

function downloadText(body: string, filename: string, type: string): void {
  const url = URL.createObjectURL(new Blob([body], { type }));
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  a.click();
  // Let the browser start consuming the object URL before releasing it.
  setTimeout(() => URL.revokeObjectURL(url), 0);
}

export default function PlatformAdminOinManifest(): JSX.Element {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Page.Section>
          <Page.Section.Title area="Platform Admin">
            OIN Cross App Access manifest
          </Page.Section.Title>
          <Page.Section.Description>
            The Speakeasy platform catalog, shaped the way Okta&apos;s
            Integration Network questionnaire wants it. Nothing here is customer
            data: only global issuers and global clients are exported.
          </Page.Section.Description>
          <Page.Section.Body>
            <StrictPlatformAdminGate>
              <ManifestView />
            </StrictPlatformAdminGate>
          </Page.Section.Body>
        </Page.Section>
      </Page.Body>
    </Page>
  );
}

function parseManifest(body: string): OinManifest | null {
  try {
    return JSON.parse(body) as OinManifest;
  } catch {
    return null;
  }
}

function ManifestView(): JSX.Element {
  const json = useManifestExport("json");
  const markdown = useManifestExport("markdown");
  const [downloading, setDownloading] = useState<ExportFormat | null>(null);

  if (json.isLoading || markdown.isLoading) {
    return (
      <div className="space-y-4" aria-busy="true" aria-label="Loading manifest">
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-48 w-full" />
      </div>
    );
  }
  const error = json.error ?? markdown.error;
  if (error || !json.data || !markdown.data) {
    return (
      <Text muted className="py-8 text-center">
        Failed to export the manifest: {error?.message ?? "empty response"}
      </Text>
    );
  }

  const manifest = parseManifest(json.data.body);
  if (!manifest) {
    return (
      <Text muted className="py-8 text-center">
        The server returned a manifest this page cannot read.
      </Text>
    );
  }

  const download = (format: ExportFormat) => {
    const data = format === "json" ? json.data : markdown.data;
    if (!data || downloading) return;
    setDownloading(format);
    try {
      downloadText(
        data.body,
        data.filename ?? defaultFilename(format, manifest.generated_at),
        CONTENT_TYPES[format],
      );
    } catch {
      toast.error("Failed to download the manifest");
    } finally {
      setDownloading(null);
    }
  };

  const blocked = manifest.resource_registrations.filter(
    (registration) => registration.blockers.length > 0,
  );

  return (
    <div className="space-y-8">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <Text muted small>
          Generated {new Date(manifest.generated_at).toLocaleString()} ·
          manifest version {manifest.manifest_version}
        </Text>
        <div className="flex gap-2">
          <Button
            variant="secondary"
            size="sm"
            icon="download"
            disabled={downloading !== null}
            onClick={() => download("markdown")}
          >
            <Button.Text>Download Markdown</Button.Text>
          </Button>
          <Button
            size="sm"
            icon="download"
            disabled={downloading !== null}
            onClick={() => download("json")}
          >
            <Button.Text>Download JSON</Button.Text>
          </Button>
        </div>
      </div>

      <MetricCard.Group>
        <MetricCard
          label="Registrations"
          value={manifest.summary.registrations}
          tone="neutral"
          description="Global issuers advertising the ID-JAG profile."
        />
        <MetricCard
          label="Ready"
          value={manifest.summary.ready}
          tone={manifest.summary.ready > 0 ? "success" : "neutral"}
          description="Pairs with no blockers left."
        />
        <MetricCard
          label="Not ready"
          value={manifest.summary.blocked}
          tone={manifest.summary.blocked > 0 ? "warning" : "neutral"}
          description="Pairs Okta would reject as submitted."
        />
      </MetricCard.Group>

      {blocked.length > 0 ? (
        <Alert variant="warning" alignTop>
          <div className="min-w-0 space-y-2">
            <Text variant="body" className="font-medium">
              Not ready: {blocked.length} registration
              {blocked.length === 1 ? "" : "s"} still carr
              {blocked.length === 1 ? "ies" : "y"} blockers
            </Text>
            <ul className="list-disc space-y-1 pl-5" aria-label="Blockers">
              {blocked.map((registration) => (
                <li key={registration.resource_as_issuer}>
                  <Text small>
                    <span className="font-medium">
                      {registration.resource_name}
                    </span>{" "}
                    <span className="text-muted-foreground font-mono">
                      {registration.resource_as_issuer}
                    </span>
                    : {registration.blockers.join("; ")}
                  </Text>
                </li>
              ))}
            </ul>
          </div>
        </Alert>
      ) : (
        <Alert variant="success">
          <Text small>Every registration is ready to submit.</Text>
        </Alert>
      )}

      <AdminSection
        title="Requesting app"
        description="Configured through GRAM_OIN_LISTING_NAME and GRAM_OIN_LISTING_ORG_DOMAIN; unset values render as unset."
      >
        <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 px-4 py-3 text-sm">
          <dt className="text-muted-foreground">Listing name</dt>
          <dd className="text-foreground">{manifest.requesting_app.name}</dd>
          <dt className="text-muted-foreground">Org domain</dt>
          <dd className="text-foreground">
            {manifest.requesting_app.org_domain}
          </dd>
          <dt className="text-muted-foreground">Role</dt>
          <dd className="text-foreground font-mono">
            {manifest.requesting_app.role} ({manifest.requesting_app.sso_mode})
          </dd>
          <dt className="text-muted-foreground">Redirect URI</dt>
          <dd className="text-foreground font-mono break-all">
            {manifest.requesting_app.redirect_uri}
          </dd>
        </dl>
      </AdminSection>

      <AdminSection
        title="Resource registrations"
        description="One row per global ID-JAG issuer and the global client Speakeasy presents to it."
      >
        <RegistrationsTable registrations={manifest.resource_registrations} />
      </AdminSection>

      <AdminSection
        title="Markdown preview"
        description="Exactly what the Markdown download contains."
      >
        <div className="prose prose-sm dark:prose-invert max-w-none overflow-x-auto px-4 py-3">
          <ReactMarkdown remarkPlugins={[remarkGfm]}>
            {markdown.data.body}
          </ReactMarkdown>
        </div>
      </AdminSection>
    </div>
  );
}

function RegistrationsTable({
  registrations,
}: {
  registrations: OinRegistration[];
}): JSX.Element {
  if (registrations.length === 0) {
    return (
      <Text muted className="px-4 py-6 text-center">
        No global issuer advertises the ID-JAG grant profile yet.
      </Text>
    );
  }

  const columns: Column<OinRegistration>[] = [
    {
      key: "resource",
      header: "Resource",
      render: (row) => (
        <div className="min-w-0">
          <Text variant="body" className="truncate font-medium">
            {row.resource_name}
          </Text>
          <Text muted small className="truncate font-mono">
            {row.resource_as_issuer}
          </Text>
        </div>
      ),
    },
    {
      key: "audience",
      header: "XAA audience",
      width: "180px",
      render: (row) =>
        row.xaa_audience ? (
          <Text small className="font-mono">
            {row.xaa_audience}
          </Text>
        ) : (
          <Text muted small>
            unknown
          </Text>
        ),
    },
    {
      key: "client",
      header: "Client ID",
      width: "200px",
      render: (row) =>
        row.client_id ? (
          <Text small className="truncate font-mono">
            {row.client_id}
          </Text>
        ) : (
          <Text muted small>
            none
          </Text>
        ),
    },
    {
      key: "scopes",
      header: "Scopes",
      width: "140px",
      render: (row) => <Text small>{row.scopes.join(", ") || "—"}</Text>,
    },
    {
      key: "registration",
      header: "Registration",
      width: "120px",
      render: (row) => (
        <Badge variant="neutral" className="shrink-0">
          <Badge.Text>{row.registration}</Badge.Text>
        </Badge>
      ),
    },
    {
      key: "status",
      header: "Status",
      width: "120px",
      render: (row) =>
        row.blockers.length === 0 ? (
          <Badge variant="success" className="shrink-0">
            <Badge.Text>Ready</Badge.Text>
          </Badge>
        ) : (
          <Badge variant="warning" background className="shrink-0">
            <Badge.Text>Not ready</Badge.Text>
          </Badge>
        ),
    },
  ];

  return (
    <Table
      columns={columns}
      data={registrations}
      rowKey={(row) => row.resource_as_issuer}
    />
  );
}
