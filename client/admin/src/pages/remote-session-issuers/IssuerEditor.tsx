import { Link } from "@tanstack/react-router";
import { invalidateIssuerQueries } from "./issuerQueries";
import { toast } from "sonner";
import type { JSX } from "react";
import { useState, useRef } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { RemoteSessionIssuer } from "@gram/admin-client/models/components/remotesessionissuer";
import type { RemoteSessionIssuerDraft } from "@gram/admin-client/models/components/remotesessionissuerdraft";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  adminCreateGlobalIssuer,
  adminUpdateGlobalIssuer,
  adminFetchGlobalIssuerMetadata,
  adminRefreshGlobalIssuerMetadata,
  adminUploadPlatformImage,
  adminGetGlobalIssuerDuplicatePreflightQuery,
} from "@/lib/gramAdminClient";
import {
  buildCreateIssuerForm,
  buildUpdateIssuerForm,
  deriveNameFromUrl,
  deriveSlugFromUrl,
  type DiscoveredEndpoints,
  type IssuerSettingsFormState,
} from "./issuerForm";
import { IssuerLogo } from "./IssuerLogo";
const endpoints = [
  "authorizationEndpoint",
  "tokenEndpoint",
  "registrationEndpoint",
  "jwksUri",
] as const;
function snapshot(
  record: RemoteSessionIssuer | RemoteSessionIssuerDraft,
): DiscoveredEndpoints {
  return {
    url: record.issuer,
    authorizationEndpoint: record.authorizationEndpoint ?? "",
    tokenEndpoint: record.tokenEndpoint ?? "",
    registrationEndpoint: record.registrationEndpoint ?? "",
    jwksUri: record.jwksUri ?? "",
    scopesSupported: record.scopesSupported ?? [],
    grantTypesSupported: record.grantTypesSupported ?? [],
    responseTypesSupported: record.responseTypesSupported ?? [],
    tokenEndpointAuthMethodsSupported:
      record.tokenEndpointAuthMethodsSupported ?? [],
    codeChallengeMethodsSupported: record.codeChallengeMethodsSupported ?? null,
    clientIdMetadataDocumentSupported: record.clientIdMetadataDocumentSupported,
    revocationEndpoint: record.revocationEndpoint ?? "",
    serviceDocumentation: record.serviceDocumentation ?? "",
    opPolicyUri: record.opPolicyUri ?? "",
    opTosUri: record.opTosUri ?? "",
    userinfoEndpoint: record.userinfoEndpoint ?? "",
    introspectionEndpoint: record.introspectionEndpoint ?? "",
    introspectionEndpointAuthMethodsSupported:
      record.introspectionEndpointAuthMethodsSupported ?? null,
    idTokenSigningAlgValuesSupported:
      record.idTokenSigningAlgValuesSupported ?? null,
    claimsSupported: record.claimsSupported ?? null,
    backchannelLogoutSupported: record.backchannelLogoutSupported ?? null,
    authorizationResponseIssParameterSupported:
      record.authorizationResponseIssParameterSupported ?? null,
  };
}
function initial(record?: RemoteSessionIssuer): IssuerSettingsFormState {
  return {
    id: record?.id ?? "",
    name: record?.name ?? "",
    slug: record?.slug ?? "",
    logoAssetId: record?.logoAssetId ?? "",
    clientSetupDocumentationUrl: record?.clientSetupDocumentationUrl ?? "",
    issuerUrl: record?.issuer ?? "",
    authorizationEndpoint: record?.authorizationEndpoint ?? "",
    tokenEndpoint: record?.tokenEndpoint ?? "",
    registrationEndpoint: record?.registrationEndpoint ?? "",
    jwksUri: record?.jwksUri ?? "",
    discoveredSnapshot: record ? snapshot(record) : null,
  };
}
export function IssuerEditor({
  issuer,
  tenantClientCount = 0,
  onDone,
  onPendingChange,
  onCreated,
}: {
  issuer?: RemoteSessionIssuer;
  tenantClientCount?: number;
  onDone: () => void;
  onPendingChange?: (pending: boolean) => void;
  onCreated?: (id: string) => Promise<void>;
}): JSX.Element | null {
  const cache = useQueryClient();
  const [form, setForm] = useState(() => initial(issuer));
  const saved = useRef(issuer);
  const [dirty, setDirty] = useState({ name: false, slug: false });
  const [pending, setPending] = useState(false);
  const busy = useRef(false);
  const [error, setError] = useState("");
  const [warnings, setWarnings] = useState<string[]>([]);
  const [discoverRan, setDiscoverRan] = useState(false);
  const [duplicateUrl, setDuplicateUrl] = useState(issuer?.issuer ?? "");
  const duplicates = useQuery({
    ...adminGetGlobalIssuerDuplicatePreflightQuery({
      issuer: duplicateUrl,
    }),
    enabled: !!duplicateUrl,
  });
  const run = async (action: () => Promise<void>) => {
    if (busy.current) return;
    busy.current = true;
    setPending(true);
    onPendingChange?.(true);
    setError("");
    try {
      await action();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Request failed");
    } finally {
      busy.current = false;
      setPending(false);
      onPendingChange?.(false);
    }
  };
  const changeUrl = (value: string) => {
    setDuplicateUrl("");
    setDiscoverRan(false);
    setForm((f) => {
      const reference = f.discoveredSnapshot?.url ?? saved.current?.issuer;
      const clear = reference !== undefined && value.trim() !== reference;
      return {
        ...f,
        issuerUrl: value,
        name:
          !issuer && !dirty.name
            ? (deriveNameFromUrl(value) ?? f.name)
            : f.name,
        slug:
          !issuer && !dirty.slug
            ? (deriveSlugFromUrl(value) ?? f.slug)
            : f.slug,
        ...(clear
          ? {
              authorizationEndpoint: "",
              tokenEndpoint: "",
              registrationEndpoint: "",
              jwksUri: "",
              discoveredSnapshot: null,
            }
          : {}),
        ...(saved.current && value.trim() === saved.current.issuer
          ? {
              ...Object.fromEntries(
                endpoints.map((key) => [key, saved.current?.[key] ?? ""]),
              ),
              discoveredSnapshot: snapshot(saved.current),
            }
          : {}),
      };
    });
    setWarnings([]);
  };
  const discover = () =>
    run(async () => {
      const draft = await adminFetchGlobalIssuerMetadata({
        issuer: form.issuerUrl.trim(),
      });
      // Fresh discovery captured omission; saved-record null still means uncaptured.
      const discovered = snapshot({
        ...draft,
        codeChallengeMethodsSupported:
          draft.codeChallengeMethodsSupported ?? [],
        introspectionEndpointAuthMethodsSupported:
          draft.introspectionEndpointAuthMethodsSupported ?? [],
        idTokenSigningAlgValuesSupported:
          draft.idTokenSigningAlgValuesSupported ?? [],
        claimsSupported: draft.claimsSupported ?? [],
      });
      setForm((f) => ({
        ...f,
        issuerUrl: draft.issuer,
        discoveredSnapshot: discovered,
        ...Object.fromEntries(
          endpoints.filter((k) => discovered[k]).map((k) => [k, discovered[k]]),
        ),
      }));
      setDuplicateUrl(draft.issuer);
      setWarnings(draft.discoveryWarnings);
      setDiscoverRan(true);
    });
  const refresh = () =>
    run(async () => {
      if (!issuer) return;
      const result = await adminRefreshGlobalIssuerMetadata({ id: issuer.id });
      saved.current = result.issuer;
      const discovered = snapshot(result.issuer);
      setForm((f) => ({
        ...f,
        issuerUrl: result.issuer.issuer,
        discoveredSnapshot: discovered,
        ...Object.fromEntries(endpoints.map((k) => [k, discovered[k]])),
      }));
      setWarnings(result.discoveryWarnings);
      setDiscoverRan(true);
      await invalidateIssuerQueries(cache);
      toast.success("Issuer metadata refreshed");
    });
  const resetAvailable =
    form.discoveredSnapshot &&
    endpoints.some(
      (k) =>
        form.discoveredSnapshot?.[k] && form[k] !== form.discoveredSnapshot[k],
    );
  const fields = [
    ["name", "Display name"],
    ["slug", "Slug"],
    ["clientSetupDocumentationUrl", "Client setup documentation URL"],
    ["authorizationEndpoint", "Authorization endpoint"],
    ["tokenEndpoint", "Token endpoint"],
    ["registrationEndpoint", "Registration endpoint"],
    ["jwksUri", "JWKS URI"],
  ] as const;
  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault();
        void run(async () => {
          const savedRecord = issuer
            ? await adminUpdateGlobalIssuer(buildUpdateIssuerForm(form))
            : await adminCreateGlobalIssuer(buildCreateIssuerForm(form));
          await invalidateIssuerQueries(cache);
          toast.success(issuer ? "Issuer saved" : "Issuer created");
          onDone();
          if (!issuer) await onCreated?.(savedRecord.id);
        });
      }}
    >
      {error && (
        <p role="alert" className="text-destructive text-sm">
          {error}
        </p>
      )}
      {discoverRan &&
        form.discoveredSnapshot &&
        !form.authorizationEndpoint.trim() && (
          <p className="text-muted-foreground text-sm">
            Authorization endpoint not advertised by the issuer.
          </p>
        )}
      {discoverRan && form.discoveredSnapshot && !form.tokenEndpoint.trim() && (
        <p className="text-muted-foreground text-sm">
          Token endpoint not advertised by the issuer.
        </p>
      )}
      {warnings.length > 0 && (
        <ul className="text-muted-foreground text-sm">
          {warnings.map((warning, i) => (
            <li key={i}>{warning}</li>
          ))}
        </ul>
      )}
      {tenantClientCount > 0 && (
        <p className="text-muted-foreground text-sm">
          Tenant clients use this issuer. Changing endpoints may disrupt
          existing integrations.
        </p>
      )}
      <fieldset disabled={pending} className="flex flex-col gap-4">
        <div className="grid gap-2">
          <label className="text-sm font-medium" htmlFor="issuer-url">
            Issuer URL
          </label>
          <Input
            id="issuer-url"
            type="url"
            required
            value={form.issuerUrl}
            onChange={(e) => changeUrl(e.target.value)}
            onBlur={() => {
              const url = form.issuerUrl.trim();
              try {
                if (["http:", "https:"].includes(new URL(url).protocol))
                  setDuplicateUrl(url);
              } catch {
                setDuplicateUrl("");
              }
            }}
          />
        </div>
        {duplicateUrl && duplicates.error && (
          <p role="alert">
            Duplicate check unavailable: {duplicates.error.message}
          </p>
        )}
        {duplicateUrl &&
          duplicates.data?.matches
            .filter((m) => m.id !== issuer?.id)
            .map((m) => (
              <p key={m.id} className="text-muted-foreground text-sm">
                Existing {m.tier} issuer: {m.name || m.slug} ({m.issuer})
                {m.projectName && ` — ${m.projectName}`}{" "}
                <Link
                  to="/remote-session-issuers/$issuerId"
                  params={{ issuerId: m.id }}
                  className="underline underline-offset-4"
                  aria-disabled={pending}
                  onClick={(e) => {
                    if (pending) e.preventDefault();
                    else if (!issuer) onDone();
                  }}
                >
                  View existing provider
                </Link>
              </p>
            ))}
        <div className="flex flex-wrap gap-2">
          {!form.discoveredSnapshot &&
            (!issuer || form.issuerUrl.trim() !== saved.current?.issuer) && (
              <Button
                type="button"
                variant="outline"
                disabled={!form.issuerUrl.trim()}
                onClick={() => void discover()}
              >
                Discover
              </Button>
            )}
          {resetAvailable && (
            <Button
              type="button"
              variant="ghost"
              onClick={() =>
                setForm((f) => ({
                  ...f,
                  ...Object.fromEntries(
                    endpoints
                      .filter((k) => f.discoveredSnapshot?.[k])
                      .map((k) => [k, f.discoveredSnapshot?.[k]]),
                  ),
                }))
              }
            >
              Reset endpoints
            </Button>
          )}
          {issuer && form.issuerUrl.trim() === saved.current?.issuer && (
            <Button
              type="button"
              variant="outline"
              disabled={form.issuerUrl.trim() !== saved.current?.issuer}
              onClick={() => void refresh()}
            >
              Refresh saved metadata
            </Button>
          )}
        </div>
        {issuer && form.issuerUrl.trim() === saved.current?.issuer && (
          <p className="text-muted-foreground text-sm">
            Refresh saves discovered metadata immediately. Other edits are not
            saved until you choose Save changes.
          </p>
        )}
        {fields.map(([key, label]) => (
          <div key={key} className="grid gap-2">
            <label className="text-sm font-medium" htmlFor={`issuer-${key}`}>
              {label}
            </label>
            <Input
              id={`issuer-${key}`}
              type={
                key.endsWith("Endpoint") ||
                key === "jwksUri" ||
                key === "clientSetupDocumentationUrl"
                  ? "url"
                  : "text"
              }
              required={key === "slug"}
              pattern={key === "slug" ? "[a-z0-9]+(?:-[a-z0-9]+)*" : undefined}
              value={form[key]}
              onChange={(e) => {
                setForm((f) => ({ ...f, [key]: e.target.value }));
                if (key === "name" || key === "slug")
                  setDirty((d) => ({ ...d, [key]: true }));
              }}
            />
          </div>
        ))}
        <div className="grid gap-2">
          <label className="text-sm font-medium" htmlFor="issuer-logo">
            Logo
          </label>
          {form.logoAssetId && <IssuerLogo id={form.logoAssetId} />}
          <Input
            id="issuer-logo"
            type="file"
            accept="image/png,image/jpeg,image/gif,image/webp"
            onChange={(e) => {
              const file = e.target.files?.[0];
              if (file)
                void run(async () => {
                  if (file.size > 4 * 1024 * 1024)
                    throw new Error("Image must be 4 MiB or smaller");
                  const result = await adminUploadPlatformImage(file);
                  setForm((f) => ({ ...f, logoAssetId: result.asset.id }));
                });
              e.target.value = "";
            }}
          />
          {form.logoAssetId && (
            <Button
              type="button"
              variant="ghost"
              onClick={() => setForm((f) => ({ ...f, logoAssetId: "" }))}
            >
              Remove logo
            </Button>
          )}
        </div>
        <div className="flex justify-end gap-2">
          <Button
            type="button"
            variant="ghost"
            onClick={() => {
              setForm(initial(saved.current));
              setDiscoverRan(false);
              setDuplicateUrl(saved.current?.issuer ?? "");
              setWarnings([]);
              setError("");
              if (!issuer) onDone();
            }}
          >
            {" "}
            {issuer ? "Discard changes" : "Cancel"}
          </Button>
          <Button type="submit">
            {pending ? "Working…" : issuer ? "Save changes" : "Create issuer"}
          </Button>
        </div>
      </fieldset>
    </form>
  );
}
