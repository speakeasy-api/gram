import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";

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
