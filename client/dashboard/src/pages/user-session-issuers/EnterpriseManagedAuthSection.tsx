import { InfoSection } from "@/components/detail-fields";
import { RequireScope } from "@/components/require-scope";
import { LabeledValue } from "@/components/setup-steps/StepBlocks";
import { Alert } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { RadioCard, RadioCardGroup } from "@/components/ui/RadioCard";
import { Text } from "@/components/ui/Text";
import { IssuerLink } from "@/lib/remote-identity/components/ProviderLink";
import { useOrgRoutes } from "@/routes";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { invalidateAllOrganizationRemoteSessionIssuers } from "@gram/client/react-query/organizationRemoteSessionIssuers.js";
import { useOrganizationRemoteSessionIssuer } from "@gram/client/react-query/organizationRemoteSessionIssuer.js";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useEffect, useId, useState } from "react";
import { Link, useLocation, useNavigate, useSearchParams } from "react-router";
import { toast } from "sonner";
import {
  lookUpProvider,
  ProviderLookupError,
  turnOffEma,
  turnOnEma,
  type FoundProvider,
} from "./ema";
import { invalidateIssuers } from "./invalidateIssuers";
import { MakeOrganizationWideDialog } from "./MakeOrganizationWideDialog";

const INTRO =
  "Let people arrive already signed in through your company's identity provider. An agent or app holding an identity token from it connects without a second sign-in. Everyone else signs in as they do today.";

type Choice = "okta" | "another";

/**
 * Enterprise Managed Auth for one MCP Authentication Provider: which
 * identity provider's sign-in it accepts. Only an organization-wide provider
 * can accept one, so a project-specific one is offered the move first.
 */
export function EnterpriseManagedAuthSection({
  issuer,
}: {
  issuer: UserSessionIssuer;
}): JSX.Element {
  const [searchParams] = useSearchParams();
  // Set when Okta's page sends the admin back with the issuer to trust.
  const returnedIssuer = searchParams.get("ema");
  const [editing, setEditing] = useState(false);

  let body: JSX.Element;
  if (issuer.projectId !== "") {
    body = <ProjectSpecific issuer={issuer} />;
  } else if (returnedIssuer && !issuer.trustedRemoteSessionClientId) {
    // Browser sign-in bound to the current identity provider pins EMA to it,
    // so a return link may not swap it here; TurnedOn says where to change it.
    body = <ConfirmReturned issuer={issuer} address={returnedIssuer} />;
  } else if (editing) {
    body = (
      <ChooseProvider issuer={issuer} onCancel={() => setEditing(false)} />
    );
  } else if (issuer.trustedRemoteSessionIssuerId) {
    body = (
      <TurnedOn
        issuer={issuer}
        trustedId={issuer.trustedRemoteSessionIssuerId}
        onChange={() => setEditing(true)}
      />
    );
  } else {
    body = <TurnedOff onSetUp={() => setEditing(true)} />;
  }

  return (
    <div>
      <InfoSection title="Enterprise Managed Auth">
        <Text small muted className="max-w-3xl">
          {INTRO}
        </Text>
        <div className="border">{body}</div>
      </InfoSection>
    </div>
  );
}

function Row({ children }: { children: React.ReactNode }): JSX.Element {
  return (
    <div className="flex flex-wrap items-center gap-4 px-6 py-5">
      {children}
    </div>
  );
}

function TurnedOff({ onSetUp }: { onSetUp: () => void }): JSX.Element {
  return (
    <Row>
      <div className="flex flex-1 basis-80 flex-col gap-1">
        <Badge variant="neutral" className="self-start">
          Off
        </Badge>
        <Text small muted>
          People sign in to Speakeasy each time they connect.
        </Text>
      </div>
      <RequireScope scope="org:admin" level="component">
        {({ disabled }) => (
          <Button disabled={disabled} onClick={onSetUp}>
            <Button.Text>Set up EMA</Button.Text>
          </Button>
        )}
      </RequireScope>
    </Row>
  );
}

function TurnedOn({
  issuer,
  trustedId,
  onChange,
}: {
  issuer: UserSessionIssuer;
  trustedId: string;
  onChange: () => void;
}): JSX.Element {
  const client = useGramContext();
  const queryClient = useQueryClient();
  const orgRoutes = useOrgRoutes();
  const trusted = useOrganizationRemoteSessionIssuer(
    { id: trustedId },
    undefined,
    { throwOnError: false },
  );
  const turnOff = useMutation({
    mutationFn: () => turnOffEma(client, issuer),
    onSuccess: async () => {
      await invalidateIssuers(queryClient);
      toast.success("Enterprise Managed Auth turned off");
    },
  });
  const providerName = trusted.isPending
    ? "Loading…"
    : trusted.data?.name || trusted.data?.issuer || "Identity provider";
  const okta = trusted.data ? isOktaIssuer(trusted.data) : false;
  // Browser sign-in also goes through this identity provider; turning EMA
  // off alone would leave that sign-in without the provider it needs.
  const signInToo = !!issuer.trustedRemoteSessionClientId;

  return (
    <Row>
      <div className="flex flex-1 basis-80 flex-col gap-2">
        <div className="flex flex-wrap items-center gap-3">
          <Badge variant="success">On</Badge>
          <Text>{providerName}</Text>
        </div>
        {trusted.isError ? (
          <Text small className="text-destructive">
            Couldn't load this identity provider. Reload the page to try again.
          </Text>
        ) : null}
        {signInToo && okta ? (
          <Text small muted>
            People also sign in to these MCP servers through this identity
            provider. Change both from the{" "}
            <Link
              className="underline"
              to={`${orgRoutes.identity.href()}?tab=identity-providers&provider=okta&view=setup`}
            >
              Okta connection
            </Link>
            .
          </Text>
        ) : null}
        {signInToo && !okta ? (
          <Text small muted>
            People also sign in to these MCP servers through this identity
            provider, so it stays on while browser sign-in uses it.
            {trusted.data ? (
              <>
                {" "}
                See <IssuerLink issuer={trusted.data} className="underline" />.
              </>
            ) : null}
          </Text>
        ) : null}
        {turnOff.isError ? (
          <Text small className="text-destructive">
            {emaChangeError("off")}
          </Text>
        ) : null}
      </div>
      {!signInToo && (
        <RequireScope scope="org:admin" level="component">
          {({ disabled }) => (
            <div className="flex gap-2">
              <Button
                variant="secondary"
                disabled={disabled}
                onClick={onChange}
              >
                <Button.Text>Change provider</Button.Text>
              </Button>
              <Button
                variant="secondary"
                disabled={disabled || turnOff.isPending}
                onClick={() => turnOff.mutate()}
              >
                <Button.Text>
                  {turnOff.isPending ? "Turning off" : "Turn off"}
                </Button.Text>
              </Button>
            </div>
          )}
        </RequireScope>
      )}
    </Row>
  );
}

function ChooseProvider({
  issuer,
  onCancel,
}: {
  issuer: UserSessionIssuer;
  onCancel: () => void;
}): JSX.Element {
  const [choice, setChoice] = useState<Choice>("okta");
  const [continued, setContinued] = useState(false);
  const orgRoutes = useOrgRoutes();
  const { pathname } = useLocation();

  if (continued && choice === "another") {
    return (
      <AnotherProvider
        issuer={issuer}
        onBack={() => setContinued(false)}
        onDone={onCancel}
      />
    );
  }

  const oktaHref = `${orgRoutes.identity.href()}?${new URLSearchParams({
    tab: "identity-providers",
    provider: "okta",
    emaFor: issuer.slug,
    emaReturn: pathname,
  }).toString()}`;

  return (
    <>
      <div className="flex flex-col gap-4 px-6 py-5">
        <Text>Which identity provider signs your people in?</Text>
        <RadioCardGroup
          aria-label="Identity provider"
          orientation="horizontal"
          value={choice}
          onValueChange={(value) => setChoice(value as Choice)}
          showIndicator={false}
        >
          <RadioCard
            value="okta"
            title="Okta"
            leading={<Icon name="key-round" className="mt-0.5 size-5" />}
          >
            Guided. Speakeasy checks your Okta domain and finds everything EMA
            needs.
          </RadioCard>
          <RadioCard
            value="another"
            title="Another identity provider"
            leading={<Icon name="fingerprint" className="mt-0.5 size-5" />}
          >
            Any OpenID Connect provider that publishes its signing keys.
          </RadioCard>
          <RadioCard
            value="speakeasy"
            title="Speakeasy"
            disabled
            leading={<Icon name="lock" className="mt-0.5 size-5" />}
          >
            Already how people sign in here.
          </RadioCard>
        </RadioCardGroup>
      </div>
      <div className="bg-muted/30 flex justify-end gap-2 border-t px-6 py-4">
        <Button variant="tertiary" onClick={onCancel}>
          <Button.Text>Cancel</Button.Text>
        </Button>
        {choice === "okta" ? (
          <Button asChild>
            <Link to={oktaHref}>
              <Button.Text>Continue with Okta</Button.Text>
            </Link>
          </Button>
        ) : (
          <Button onClick={() => setContinued(true)}>
            <Button.Text>Continue</Button.Text>
          </Button>
        )}
      </div>
    </>
  );
}

function useLookUp() {
  const client = useGramContext();
  return useMutation<FoundProvider, Error, string>({
    mutationFn: (address) => lookUpProvider(client, address),
  });
}

function useTurnOn(issuer: UserSessionIssuer, onDone: () => void) {
  const client = useGramContext();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (found: FoundProvider) => turnOnEma(client, issuer, found),
    onSuccess: async () => {
      await Promise.all([
        invalidateIssuers(queryClient),
        invalidateAllOrganizationRemoteSessionIssuers(queryClient),
      ]);
      toast.success("Enterprise Managed Auth turned on");
      onDone();
    },
  });
}

function emaChangeError(to: "on" | "off"): string {
  return `Couldn't turn Enterprise Managed Auth ${to}. Try again.`;
}

/**
 * Okta's own setup names the identity provider it creates "okta-sign-in…";
 * otherwise, Okta's org authorization server lives on an Okta-run host.
 */
function isOktaIssuer({
  slug,
  issuer,
}: {
  slug?: string | undefined;
  issuer: string;
}): boolean {
  if (slug?.startsWith("okta-sign-in")) return true;
  try {
    return /\.(okta|oktapreview|okta-emea)\.com$/i.test(
      new URL(issuer).hostname,
    );
  } catch {
    return false;
  }
}

function lookupMessage(error: Error | null): string | null {
  if (!error) return null;
  return error instanceof ProviderLookupError
    ? error.message
    : "Couldn't look up this provider. Try again.";
}

function AnotherProvider({
  issuer,
  onBack,
  onDone,
}: {
  issuer: UserSessionIssuer;
  onBack: () => void;
  onDone: () => void;
}): JSX.Element {
  const inputId = useId();
  const [address, setAddress] = useState("");
  const lookUp = useLookUp();
  const turnOn = useTurnOn(issuer, onDone);
  const found = lookUp.data;

  return (
    <>
      <div className="flex flex-col gap-5 px-6 py-5">
        <form
          className="flex max-w-2xl flex-col gap-2"
          onSubmit={(event) => {
            event.preventDefault();
            lookUp.mutate(address);
          }}
        >
          <Label htmlFor={inputId}>Identity provider address</Label>
          <div className="flex gap-2">
            <Input
              id={inputId}
              value={address}
              placeholder="https://login.example.com"
              onChange={(value: string) => {
                setAddress(value);
                // A lookup describes the address it ran on; once the field
                // changes, nothing may be turned on with the old result.
                lookUp.reset();
              }}
            />
            <Button
              type="submit"
              variant="secondary"
              disabled={lookUp.isPending || address.trim() === ""}
            >
              <Button.Text>
                {lookUp.isPending ? "Looking up" : "Look up"}
              </Button.Text>
            </Button>
          </div>
          <Text small muted>
            The issuer your provider names in its identity tokens.
          </Text>
          {lookUp.isError ? (
            <Text small className="text-destructive">
              {lookupMessage(lookUp.error)}
            </Text>
          ) : null}
        </form>
        {found ? <FoundDetails issuer={issuer} found={found} /> : null}
        {turnOn.isError ? (
          <Text small className="text-destructive">
            {emaChangeError("on")}
          </Text>
        ) : null}
      </div>
      <div className="bg-muted/30 flex justify-end gap-2 border-t px-6 py-4">
        <Button variant="tertiary" onClick={onBack}>
          <Button.Text>Back</Button.Text>
        </Button>
        <Button
          disabled={!found || turnOn.isPending}
          onClick={() => found && turnOn.mutate(found)}
        >
          <Button.Text>
            {turnOn.isPending ? "Turning on" : "Turn on EMA"}
          </Button.Text>
        </Button>
      </div>
    </>
  );
}

function FoundDetails({
  issuer,
  found,
}: {
  issuer: UserSessionIssuer;
  found: FoundProvider;
}): JSX.Element {
  return (
    <>
      <Alert variant="success" title="Found" dismissible={false}>
        {new URL(found.issuer).hostname} publishes its signing keys, so
        Speakeasy can check the identity it vouches for.
      </Alert>
      {issuer.issuerUrl ? (
        <div className="flex max-w-2xl flex-col gap-1">
          <LabeledValue
            label="Audience to set in your provider"
            value={issuer.issuerUrl}
          />
          <Text small muted>
            Your provider names this address when it issues a token for
            Speakeasy.
          </Text>
        </div>
      ) : null}
    </>
  );
}

/** Back from Okta's page: confirm the issuer it found, then turn EMA on. */
function ConfirmReturned({
  issuer,
  address,
}: {
  issuer: UserSessionIssuer;
  address: string;
}): JSX.Element {
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const lookUp = useLookUp();
  const done = () => void navigate(pathname, { replace: true });
  const turnOn = useTurnOn(issuer, done);
  const { mutate } = lookUp;

  useEffect(() => {
    mutate(address);
  }, [address, mutate]);

  const found = lookUp.data;
  return (
    <>
      <div className="flex flex-col gap-4 px-6 py-5">
        <Text>Turn on Enterprise Managed Auth with {address}?</Text>
        {lookUp.isPending ? (
          <Text small muted>
            Checking {address}…
          </Text>
        ) : null}
        {lookUp.isError ? (
          <Text small className="text-destructive">
            {lookupMessage(lookUp.error)}
          </Text>
        ) : null}
        {found ? <FoundDetails issuer={issuer} found={found} /> : null}
        {turnOn.isError ? (
          <Text small className="text-destructive">
            {emaChangeError("on")}
          </Text>
        ) : null}
      </div>
      <div className="bg-muted/30 flex justify-end gap-2 border-t px-6 py-4">
        <Button variant="tertiary" onClick={done}>
          <Button.Text>Cancel</Button.Text>
        </Button>
        <RequireScope scope="org:admin" level="component">
          {({ disabled }) => (
            <Button
              disabled={disabled || !found || turnOn.isPending}
              onClick={() => found && turnOn.mutate(found)}
            >
              <Button.Text>
                {turnOn.isPending ? "Turning on" : "Turn on EMA"}
              </Button.Text>
            </Button>
          )}
        </RequireScope>
      </div>
    </>
  );
}

function ProjectSpecific({
  issuer,
}: {
  issuer: UserSessionIssuer;
}): JSX.Element {
  const [promoting, setPromoting] = useState(false);
  return (
    <Row>
      <div className="flex flex-1 basis-80 flex-col gap-1">
        <Text>
          Enterprise Managed Auth needs an organization-wide MCP Authentication
          Provider
        </Text>
        <Text small muted>
          Make {issuer.slug} organization-wide first. Its MCP servers, sessions
          and sign-in address stay as they are.
        </Text>
      </div>
      <RequireScope scope="org:admin" level="component">
        {({ disabled }) => (
          <Button disabled={disabled} onClick={() => setPromoting(true)}>
            <Button.Text>Make organization-wide</Button.Text>
          </Button>
        )}
      </RequireScope>
      {promoting && (
        <MakeOrganizationWideDialog
          issuer={issuer}
          onClose={() => setPromoting(false)}
        />
      )}
    </Row>
  );
}
