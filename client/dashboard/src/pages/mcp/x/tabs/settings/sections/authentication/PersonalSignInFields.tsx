import { Alert } from "@/components/ui/Alert";
import { Combobox } from "@/components/ui/Combobox";
import { FieldError } from "@/components/ui/Field";
import { Label } from "@/components/ui/Label";
import { Text } from "@/components/ui/Text";
import { IssuerLogo, RadioRows } from "@/lib/remote-identity";
import { useRoutes } from "@/routes";
import { Link } from "react-router";
import { AuthorizationServerSelect } from "./AuthorizationServerSelect";
import { ClientTypeFields, OverridesFields } from "./IssuerFormFields";
import { TUNNELED_DCR_PERMISSION_MESSAGE } from "./issuerFormUtils";
import { StartingClientFields } from "./StartingClientFields";
import type {
  PersonalSignInDraft,
  PersonalSignInSource,
} from "./usePersonalSignInDraft";

const NO_EXCLUDED_ISSUERS: ReadonlySet<string> = new Set();

/**
 * The sign-in a personal credential gets: from scratch, or started from a
 * stored one whose settings fill the form for review.
 */
export function PersonalSignInFields({
  draft,
}: {
  draft: PersonalSignInDraft;
}): JSX.Element {
  const routes = useRoutes();
  const f = draft.fields;
  const { signIns } = f;
  const picked = f.templates.find(
    (choice) => choice.registration.id === f.templateId,
  );

  let existingBody: JSX.Element;
  if (signIns.isLoading) {
    existingBody = (
      <Text muted small>
        Loading sign-ins…
      </Text>
    );
  } else if (signIns.isError) {
    existingBody = (
      <FieldError>
        Sign-ins could not be loaded. Refresh the page to try again.
      </FieldError>
    );
  } else if (f.templates.length === 0) {
    existingBody = (
      <Alert variant="info" inline>
        No sign-in for people&apos;s own accounts
        {signIns.upstreamIssuers ? ` with ${signIns.upstreamIssuers}` : ""} can
        be used as a starting point yet. Choose New from scratch instead.
      </Alert>
    );
  } else {
    existingBody = (
      <Combobox
        id="credential-starting-sign-in"
        items={f.templates.map((choice) => ({
          value: choice.registration.id,
          label: choice.label,
          description: choice.description,
          keywords: [choice.description],
          icon: (
            <IssuerLogo
              logoAssetId={choice.issuer?.logoAssetId}
              name={choice.label}
              size="sm"
            />
          ),
        }))}
        selected={f.templateId}
        onSelectionChange={(item) => f.selectTemplate(item.value)}
        searchPlaceholder="Search sign-ins…"
        emptyMessage="No sign-ins match."
        className="w-full justify-between"
        contentClassName="w-[min(500px,calc(100vw-2rem))]"
      >
        {picked ? (
          <span className="flex min-w-0 items-center gap-2">
            <IssuerLogo
              logoAssetId={picked.issuer?.logoAssetId}
              name={picked.label}
              size="sm"
            />
            <span className="truncate">{picked.label}</span>
            <span className="text-muted-foreground truncate font-normal">
              {picked.description}
            </span>
          </span>
        ) : (
          "Choose a sign-in"
        )}
      </Combobox>
    );
  }

  const overrides = (
    <OverridesFields
      scopeOverride={f.scope}
      audienceOverride={f.audience}
      onScopeOverrideChange={f.setScope}
      onAudienceOverrideChange={f.setAudience}
    />
  );
  return (
    <div className="space-y-4">
      <RadioRows
        label="Sign-in (required)"
        value={f.source}
        onValueChange={(value) => f.setSource(value as PersonalSignInSource)}
        rows={[
          {
            value: "scratch",
            title: "New from scratch",
            description: "Set up a new sign-in with an authorization server.",
          },
          {
            value: "existing",
            title: "New from existing",
            description:
              "Copy a stored sign-in's settings into an independent one.",
          },
        ]}
      />

      {f.source === "existing" ? (
        <div className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="credential-starting-sign-in">
              Start from (required)
            </Label>
            {existingBody}
            {!signIns.isLoading &&
            !signIns.isError &&
            signIns.upstreamIssuers &&
            (signIns.hiddenCount > 0 || signIns.showAll) ? (
              <Text muted small className="block">
                {signIns.showAll
                  ? "Showing every sign-in, including ones for other services. "
                  : f.templates.length > 0
                    ? `Showing sign-ins for ${signIns.upstreamIssuers}, as named by the upstream. `
                    : ""}
                <button
                  type="button"
                  className="font-medium underline underline-offset-2"
                  onClick={f.toggleShowAll}
                >
                  {signIns.showAll
                    ? `Show only ${signIns.upstreamIssuers}`
                    : "Show all"}
                </button>
              </Text>
            ) : null}
            <Text muted small className="block">
              This creates an independent copy of its settings, not a shared
              sign-in. The sign-in you start from, the servers that use it, and
              the people signed in through it stay as they are. People sign in
              again for this credential.
            </Text>
          </div>
          {f.starting ? (
            <>
              <StartingClientFields
                starting={f.starting}
                clientId={f.clientId}
                clientSecret={f.clientSecret}
                tokenEndpointAuthMethod={f.authMethod}
                onClientIdChange={f.setClientId}
                onClientSecretChange={f.setClientSecret}
                onTokenEndpointAuthMethodChange={f.setAuthMethod}
              />
              {overrides}
            </>
          ) : null}
        </div>
      ) : (
        <div className="space-y-4">
          <AuthorizationServerSelect
            excludedIssuerIds={NO_EXCLUDED_ISSUERS}
            selectedIssuerId={f.issuerId}
            selectedIssuer={f.issuer}
            pickFailed={f.issuerFailed}
            onChange={f.selectIssuer}
          />
          <Text muted small className="block">
            Not listed? Add it in{" "}
            <Link
              className="font-medium underline underline-offset-2"
              to={routes.remoteIdentityProviders.href()}
            >
              Authorization Servers
            </Link>
            , then come back.
          </Text>
          {f.issuer ? (
            <>
              <ClientTypeFields
                availableTypes={f.clientTypes}
                clientType={f.clientType}
                onClientTypeChange={f.setClientType}
                clientId={f.clientId}
                clientSecret={f.clientSecret}
                tokenEndpointAuthMethod={f.authMethod}
                onClientIdChange={f.setClientId}
                onClientSecretChange={f.setClientSecret}
                onTokenEndpointAuthMethodChange={f.setAuthMethod}
              />
              {f.tunneledDcrRestricted ? (
                <Text muted small className="block">
                  {TUNNELED_DCR_PERMISSION_MESSAGE}
                </Text>
              ) : null}
              {overrides}
            </>
          ) : null}
        </div>
      )}
    </div>
  );
}
