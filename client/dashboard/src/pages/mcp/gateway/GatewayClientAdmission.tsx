import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useProject } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { useRoutes } from "@/routes";
import { useUserSessionIssuer } from "@gram/client/react-query/userSessionIssuer.js";
import { useUserSessionIssuerCimdClients } from "@gram/client/react-query/userSessionIssuerCimdClients.js";
import { Link } from "react-router";
import { isUnavailable } from "./connectionHealth";
import { GATEWAY_AUTHENTICATION_SECTION_ID } from "./GatewaySettingsTab";

// The custom document URLs shown before saying more are configured.
const CUSTOM_URL_LIMIT = 10;

type ModeCopy = { label: string; meaning: string; restricted: boolean };

const OPEN: ModeCopy = {
  label: "Open",
  meaning:
    "Any app with a valid client ID metadata document can sign in. The document is still checked.",
  restricted: false,
};

// The effective modes the issuer read reports; reporting admits what open
// does. A Map, so no other value, inherited keys included, reads as known.
const MODES = new Map<string, ModeCopy>([
  ["open", OPEN],
  ["reporting", OPEN],
  [
    "presets",
    {
      label: "Known apps only",
      meaning:
        "Only Speakeasy's reviewed list of apps, plus the custom document URLs below, can sign in with a client ID metadata document. Other apps are turned away.",
      restricted: true,
    },
  ],
  [
    "disabled",
    {
      label: "Off",
      meaning:
        "Client ID metadata documents are not accepted or advertised. Apps can still register dynamically.",
      restricted: true,
    },
  ],
]);

/**
 * The gateway's current setting for apps that sign in with a client ID
 * metadata document, read from its sign-in server. Anything short of a
 * settled read of a known mode is shown as unknown, never as open.
 */
export function GatewayClientAdmission({
  gatewayId,
  issuerId,
}: {
  gatewayId: string;
  issuerId: string | null | undefined;
}): JSX.Element {
  const issuer = useUserSessionIssuer({ id: issuerId ?? "" }, undefined, {
    enabled: !!issuerId,
    throwOnError: false,
    retry: false,
  });
  // A failed refetch keeps its earlier data; the setting is unknown then, so
  // that data never stands for it.
  const settled = issuerId && !issuer.isError ? issuer.data : undefined;
  const mode = MODES.get(settled?.clientIdMetadataAdmissionMode ?? "");

  let body: JSX.Element;
  if (!issuerId) {
    body = (
      <Text small muted>
        This gateway has no sign-in server, so no client admission setting
        applies.
      </Text>
    );
  } else if (isUnavailable(issuer.error)) {
    body = (
      <Text small muted>
        The sign-in server isn&apos;t visible to you, so the setting is unknown.
      </Text>
    );
  } else if (issuer.isError) {
    body = (
      <div className="flex items-center justify-between gap-3">
        <Text small className="text-destructive">
          Couldn&apos;t load the setting, so it is unknown.
        </Text>
        <Button
          variant="tertiary"
          size="sm"
          onClick={() => void issuer.refetch()}
        >
          Retry
        </Button>
      </div>
    );
  } else if (!settled) {
    body = <Skeleton className="h-12 w-full" />;
  } else if (!mode) {
    body = (
      <Text small muted>
        The sign-in server reports a setting this page doesn&apos;t recognize,
        so it is unknown.
      </Text>
    );
  } else {
    body = (
      <>
        <Text small className="block">
          <span className="font-medium">{mode.label}.</span> {mode.meaning}
        </Text>
        {settled.clientIdMetadataAdmissionMode === "presets" && (
          <CustomURLs issuerId={issuerId} />
        )}
        <Text small muted className="block">
          This is the setting in force now for apps that sign in with a client
          ID metadata document. It doesn&apos;t revoke existing connections or
          change dynamic client registration, and existing connections may have
          been admitted under an earlier setting.
        </Text>
        <SettingsLink
          gatewayId={gatewayId}
          editable={settled.projectId !== ""}
        />
      </>
    );
  }

  return (
    <section className="space-y-2">
      <div className="flex items-center gap-2">
        <h3 className="text-eyebrow">Client admission</h3>
        {mode?.restricted && <Badge variant="warning">Restricted</Badge>}
      </div>
      {body}
    </section>
  );
}

function CustomURLs({ issuerId }: { issuerId: string }): JSX.Element {
  const urls = useUserSessionIssuerCimdClients(
    // One past the limit says whether more exist: the list's own cursor is
    // set whenever a page is full.
    { userSessionIssuerId: issuerId, limit: CUSTOM_URL_LIMIT + 1 },
    undefined,
    { throwOnError: false, retry: false },
  );
  if (urls.isError) {
    return (
      <div className="flex items-center justify-between gap-3">
        <Text small className="text-destructive">
          Couldn&apos;t load the custom document URLs.
        </Text>
        <Button
          variant="tertiary"
          size="sm"
          onClick={() => void urls.refetch()}
        >
          Retry
        </Button>
      </div>
    );
  }
  if (!urls.data) return <Skeleton className="h-8 w-full" />;
  const shown = urls.data.result.items.slice(0, CUSTOM_URL_LIMIT);
  if (shown.length === 0) {
    return (
      <Text small muted className="block">
        No custom document URLs are configured.
      </Text>
    );
  }
  return (
    <div className="space-y-1">
      <ul className="space-y-0.5">
        {shown.map((client) => (
          <li key={client.id}>
            <Text
              small
              className="block font-mono break-all"
              title={client.clientIdMetadataUri}
            >
              {client.clientIdMetadataUri}
            </Text>
          </li>
        ))}
      </ul>
      {urls.data.result.items.length > CUSTOM_URL_LIMIT && (
        <Text small muted className="block">
          More custom document URLs are configured than are listed here.
        </Text>
      )}
    </div>
  );
}

// The gateway's own authentication settings, where the setting is changed.
// An organization-owned sign-in server is shown there read-only.
function SettingsLink({
  gatewayId,
  editable,
}: {
  gatewayId: string;
  editable: boolean;
}): JSX.Element {
  const routes = useRoutes();
  const project = useProject();
  const { hasScope } = useRBAC();
  let label = "View in gateway settings";
  if (editable && hasScope("project:write", project.id)) {
    label = "Change in gateway settings";
  }
  return (
    <Link
      to={`${routes.mcp.gateway.settings.href(gatewayId)}#${GATEWAY_AUTHENTICATION_SECTION_ID}`}
      className="text-sm underline"
    >
      {label}
    </Link>
  );
}
