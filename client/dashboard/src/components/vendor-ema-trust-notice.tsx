import { Alert } from "@/components/ui/Alert";
import { Text } from "@/components/ui/Text";

/** Reminds admins that each vendor must also trust their identity provider; Speakeasy cannot do this step. */
export function VendorEmaTrustNotice({
  issuerUrl,
}: {
  /** The identity provider issuer the vendor must trust, when known. */
  issuerUrl?: string;
}): JSX.Element {
  return (
    <Alert variant="info" alignTop>
      <Text small>
        Each vendor must also enable enterprise-managed authorization in its own
        admin console and trust your identity provider&apos;s issuer
        {issuerUrl ? (
          <>
            {" "}
            (<span className="font-mono">{issuerUrl}</span>)
          </>
        ) : null}
        . For example, with Okta, Linear needs SAML via Okta and &quot;MCP
        enterprise managed authentication&quot; turned on with your Okta issuer.
        Many vendors require an Enterprise or SSO plan, and some only allow
        specific clients. Speakeasy cannot do this step for you.
      </Text>
    </Alert>
  );
}
