import type { RemoteSessionIssuerDraft } from "@gram/client/models/components/remotesessionissuerdraft.js";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";

export function VerifiedOAuthMetadata({
  metadata,
}: {
  metadata: RemoteSessionIssuerDraft;
}): React.JSX.Element {
  return (
    <Stack gap={4}>
      {!!metadata.discoveryWarnings?.length && (
        <Alert variant="warning">
          <AlertTitle>Metadata warnings</AlertTitle>
          <AlertDescription>
            {metadata.discoveryWarnings.join(" ")}
          </AlertDescription>
        </Alert>
      )}
      <MetadataRow label="Issuer URL" value={metadata.issuer} />
      <MetadataRow
        label="Authorization endpoint"
        value={metadata.authorizationEndpoint ?? "Not advertised"}
      />
      <MetadataRow
        label="Token endpoint"
        value={metadata.tokenEndpoint ?? "Not advertised"}
      />
      <Alert
        variant={
          metadata.authorizationResponseIssParameterSupported
            ? "success"
            : "error"
        }
      >
        <a
          href="https://www.rfc-editor.org/rfc/rfc9207.html"
          target="_blank"
          rel="noreferrer"
          className="underline underline-offset-2"
        >
          RFC 9207
        </a>{" "}
        {metadata.authorizationResponseIssParameterSupported
          ? "Supported"
          : "Unsupported"}
      </Alert>
    </Stack>
  );
}

function MetadataRow({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <Text small className="font-medium">
        {label}
      </Text>
      <Text muted small className="break-all">
        {value}
      </Text>
    </div>
  );
}
