import { Alert } from "@/components/ui/Alert";
import { Text } from "@/components/ui/Text";

/** Reminds admins that each vendor must also trust the issuer that vouches for sign-ins; Speakeasy cannot do this step. */
export function VendorEmaTrustNotice({
  issuerUrl,
  speakeasy = false,
}: {
  /** The issuer the vendor must trust, when known. */
  issuerUrl?: string;
  /** Speakeasy's own issuer vouches, rather than the organization's identity provider. */
  speakeasy?: boolean;
}): JSX.Element {
  const issuer = issuerUrl ? (
    <>
      {" "}
      (<span className="font-mono">{issuerUrl}</span>)
    </>
  ) : null;
  if (speakeasy) {
    return (
      <Alert variant="info" alignTop>
        <Text small>
          Each vendor must also enable enterprise-managed authorization in its
          own admin console and trust Speakeasy&apos;s issuer{issuer}. Many
          vendors require an Enterprise or SSO plan, and some only allow
          specific clients. Speakeasy cannot do this step for you.
        </Text>
      </Alert>
    );
  }
  return (
    <Alert variant="info" alignTop>
      <Text small>
        Each vendor must also enable enterprise-managed authorization in its own
        admin console and trust your identity provider&apos;s issuer
        {issuer}. For example, with Okta, Linear needs SAML via Okta and
        &quot;MCP enterprise managed authentication&quot; turned on with your
        Okta issuer. Many vendors require an Enterprise or SSO plan, and some
        only allow specific clients. Speakeasy cannot do this step for you.
      </Text>
    </Alert>
  );
}
