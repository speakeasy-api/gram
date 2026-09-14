import type { JSX, ReactNode } from "react";
import type { RemoteSessionIssuer } from "@gram/admin-client/models/components/remotesessionissuer";
function Field({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <div>
      <dt className="text-muted-foreground text-sm">{label}</dt>
      <dd className="break-all text-sm">{children}</dd>
    </div>
  );
}
function Documentation({ value }: { value?: string }): JSX.Element {
  const url = value?.trim();
  let safe = false;
  try {
    safe = !!url && ["http:", "https:"].includes(new URL(url).protocol);
  } catch {
    /* Stored invalid values remain visible, never executable links. */
  }
  return safe ? (
    <a
      href={url}
      target="_blank"
      rel="noopener noreferrer"
      className="underline underline-offset-4"
    >
      {url}
    </a>
  ) : (
    <>{url || "—"}</>
  );
}
const ordinary = (v?: string | null): string => v?.trim() || "—";
const list = (v?: string[] | null): string => (v?.length ? v.join(", ") : "—");
const captured = (v?: string[] | null): string =>
  v == null ? "Not captured" : v.length ? v.join(", ") : "None advertised";
const supported = (v?: boolean): string =>
  v === undefined ? "Not captured" : v ? "Supported" : "Not supported";
export function IssuerConfiguration({
  issuer,
}: {
  issuer: RemoteSessionIssuer;
}): JSX.Element {
  const sections: [string, [string, ReactNode][]][] = [
    [
      "Essentials",
      [
        ["Name", ordinary(issuer.name)],
        ["Slug", ordinary(issuer.slug)],
        ["Issuer", ordinary(issuer.issuer)],
        ["Issuer ID", issuer.id],
      ],
    ],
    [
      "Endpoints",
      [
        ["Authorization", ordinary(issuer.authorizationEndpoint)],
        ["Token", ordinary(issuer.tokenEndpoint)],
        ["Registration", ordinary(issuer.registrationEndpoint)],
        ["JWKS", ordinary(issuer.jwksUri)],
        ["Revocation", ordinary(issuer.revocationEndpoint)],
        ["Userinfo", ordinary(issuer.userinfoEndpoint)],
        ["Introspection", ordinary(issuer.introspectionEndpoint)],
      ],
    ],
    [
      "Identity Provider Details",
      [
        ["Scopes", list(issuer.scopesSupported)],
        ["Grant Types", list(issuer.grantTypesSupported)],
        ["Response Types", list(issuer.responseTypesSupported)],
        [
          "Token Endpoint Authentication Methods",
          list(issuer.tokenEndpointAuthMethodsSupported),
        ],
        [
          "PKCE Code Challenge Methods",
          captured(issuer.codeChallengeMethodsSupported),
        ],
        [
          "Client ID Metadata Document",
          supported(issuer.clientIdMetadataDocumentSupported),
        ],
        [
          "Introspection Authentication Methods",
          captured(issuer.introspectionEndpointAuthMethodsSupported),
        ],
        [
          "ID Token Signing Algorithms",
          captured(issuer.idTokenSigningAlgValuesSupported),
        ],
        ["Claims", captured(issuer.claimsSupported)],
        ["Back-Channel Logout", supported(issuer.backchannelLogoutSupported)],
        [
          "Authorization Response Issuer Parameter",
          supported(issuer.authorizationResponseIssParameterSupported),
        ],
        ["OpenID Connect", supported(issuer.oidc)],
        ["Passthrough", supported(issuer.passthrough)],
        ["Resource Indicator", supported(issuer.resourceIndicatorSupported)],
        ["Scope Override", list(issuer.scopeOverride)],
      ],
    ],
    [
      "Documentation",
      [
        [
          "Client Setup Documentation",
          <Documentation
            key="setup"
            value={issuer.clientSetupDocumentationUrl}
          />,
        ],
        [
          "Service Documentation",
          <Documentation key="service" value={issuer.serviceDocumentation} />,
        ],
        ["Policy", <Documentation key="policy" value={issuer.opPolicyUri} />],
        [
          "Terms of Service",
          <Documentation key="terms" value={issuer.opTosUri} />,
        ],
      ],
    ],
    [
      "Record",
      [
        ["Created", issuer.createdAt?.toLocaleString() || "—"],
        ["Updated", issuer.updatedAt?.toLocaleString() || "—"],
      ],
    ],
  ];
  return (
    <div className="grid gap-6 sm:grid-cols-2">
      {sections.map(([title, fields]) => (
        <section key={title} className="grid content-start gap-3">
          <h2 className="text-base font-semibold">{title}</h2>
          <dl className="grid gap-3">
            {fields.map(([label, value]) => (
              <Field key={label} label={label}>
                {value}
              </Field>
            ))}
          </dl>
        </section>
      ))}
    </div>
  );
}
