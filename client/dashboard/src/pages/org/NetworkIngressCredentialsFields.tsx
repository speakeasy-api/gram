import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";

// Where a tailnet admin creates the OAuth client Speakeasy needs.
export const TAILSCALE_TRUST_CREDENTIALS_URL =
  "https://console.tailscale.com/admin/settings/trust-credentials";

export function TailscaleTrustCredentialsLink(): JSX.Element {
  return (
    <a
      href={TAILSCALE_TRUST_CREDENTIALS_URL}
      target="_blank"
      rel="noopener noreferrer"
      className="text-foreground underline underline-offset-4"
    >
      Trust credentials
    </a>
  );
}

export function NetworkIngressCredentialsFields({
  idPrefix,
  clientId,
  clientSecret,
  onClientIdChange,
  onClientSecretChange,
}: {
  idPrefix: string;
  clientId: string;
  clientSecret: string;
  onClientIdChange: (value: string) => void;
  onClientSecretChange: (value: string) => void;
}): JSX.Element {
  return (
    <>
      <div className="space-y-2">
        <Label htmlFor={`${idPrefix}-client-id`}>OAuth client ID</Label>
        <Input
          id={`${idPrefix}-client-id`}
          value={clientId}
          onChange={onClientIdChange}
          autoComplete="off"
        />
      </div>
      <div className="space-y-2">
        <Label htmlFor={`${idPrefix}-client-secret`}>OAuth client secret</Label>
        <Input
          id={`${idPrefix}-client-secret`}
          value={clientSecret}
          onChange={onClientSecretChange}
          reveal
          autoComplete="new-password"
        />
      </div>
    </>
  );
}
