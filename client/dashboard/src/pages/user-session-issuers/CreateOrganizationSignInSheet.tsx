import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/RadioGroup";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { useCreateOrganizationUserSessionIssuerMutation } from "@gram/client/react-query/createOrganizationUserSessionIssuer.js";
import { invalidateAllOrganizationGateway } from "@gram/client/react-query/organizationGateway.js";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { Link } from "react-router";
import { toast } from "sonner";
import {
  DEFAULT_SESSION_HOURS,
  SESSION_LENGTH_OPTIONS,
  joinSignInNames,
  signInNameError,
  signInSlug,
} from "./createOrganizationSignIn";
import { invalidateIssuers } from "./invalidateIssuers";
import type { IssuerTier } from "./useIssuerTier";

type LoginMethod = "speakeasy" | "okta";

/** Creates an organization-wide sign-in any project's MCP servers can use. */
export function CreateOrganizationSignInSheet({
  open,
  onOpenChange,
  organizationTier,
  oktaSetupHref,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  organizationTier: Pick<
    IssuerTier,
    "items" | "isLoading" | "isError" | "isFetching" | "retry" | "refresh"
  >;
  /** Where Okta-backed sign-ins are set up. */
  oktaSetupHref: string;
  onCreated: (issuer: UserSessionIssuer) => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const [method, setMethod] = useState<LoginMethod | null>(null);
  const [name, setName] = useState("");
  const [hours, setHours] = useState(DEFAULT_SESSION_HOURS);
  const [acknowledged, setAcknowledged] = useState(false);
  // Another admin may have created one since the list loaded.
  const [refreshing, setRefreshing] = useState(false);
  const slug = signInSlug(name);
  const nameError = signInNameError(name, organizationTier.items);
  const existingNames = joinSignInNames(
    organizationTier.items.map((issuer) => issuer.slug),
  );
  const hasExisting = organizationTier.items.length > 0;
  // The name check and the gateway warning both need the whole, current list.
  const tierReady =
    !organizationTier.isLoading &&
    !organizationTier.isError &&
    !organizationTier.isFetching &&
    !refreshing;

  const create = useCreateOrganizationUserSessionIssuerMutation({
    onSuccess: async (issuer: UserSessionIssuer) => {
      await Promise.all([
        invalidateIssuers(queryClient),
        // One organization-wide sign-in more can turn the gateway on or off.
        invalidateAllOrganizationGateway(queryClient),
      ]);
      toast.success(`Created ${issuer.slug}`);
      onOpenChange(false);
      onCreated(issuer);
    },
  });
  const { reset } = create;
  // Held in a ref so a new function each render doesn't refetch again.
  const refresh = useRef(organizationTier.refresh);
  refresh.current = organizationTier.refresh;

  useEffect(() => {
    if (!open) return;
    setMethod(null);
    setName("");
    setHours(DEFAULT_SESSION_HOURS);
    setAcknowledged(false);
    reset();
    let current = true;
    setRefreshing(true);
    void refresh.current().finally(() => {
      if (current) setRefreshing(false);
    });
    return () => {
      current = false;
    };
  }, [open, reset]);

  const canCreate =
    method === "speakeasy" &&
    tierReady &&
    slug !== "" &&
    !nameError &&
    (!hasExisting || acknowledged) &&
    !create.isPending;

  return (
    <Sheet
      open={open}
      onOpenChange={(next) => {
        if (!create.isPending) onOpenChange(next);
      }}
    >
      <SheetContent
        side="right"
        className="flex w-[560px] max-w-[calc(100vw-2rem)] flex-col sm:max-w-[560px]"
      >
        <SheetHeader className="px-6 pt-6 pb-0">
          <SheetTitle className="text-lg font-semibold">
            Create organization-wide sign-in
          </SheetTitle>
          <SheetDescription>
            MCP servers in every project can use it. Enterprise Managed
            Authorization needs one.
          </SheetDescription>
        </SheetHeader>

        <div className="flex-1 overflow-y-auto px-6 py-6">
          <Stack gap={6}>
            <Stack gap={3}>
              <Label id="org-sign-in-method">How do people log in?</Label>
              <RadioGroup
                aria-labelledby="org-sign-in-method"
                value={method ?? ""}
                onValueChange={(value) => setMethod(value as LoginMethod)}
                disabled={create.isPending}
              >
                <label className="flex items-start gap-3">
                  <RadioGroupItem value="speakeasy" className="mt-0.5" />
                  <span>
                    <Text as="span" className="block">
                      Speakeasy login
                    </Text>
                    <Text small muted as="span" className="block">
                      People log in with their Speakeasy account.
                    </Text>
                  </span>
                </label>
                <label className="flex items-start gap-3">
                  <RadioGroupItem value="okta" className="mt-0.5" />
                  <span>
                    <Text as="span" className="block">
                      Okta
                    </Text>
                    <Text small muted as="span" className="block">
                      People log in through your company’s Okta.
                    </Text>
                  </span>
                </label>
              </RadioGroup>
            </Stack>

            {method === "okta" && (
              <Text small>
                {hasExisting
                  ? `To keep the organization gateway on, open Okta sign-in and choose Trust Okta sign-in on ${existingNames} instead of creating a new one.`
                  : "Okta sign-in is set up from your Okta connection. You need a verified Okta connection first."}
              </Text>
            )}

            {method === "speakeasy" && (
              <>
                <Stack gap={2}>
                  <Label htmlFor="org-sign-in-name">Name</Label>
                  <Input
                    id="org-sign-in-name"
                    value={name}
                    onChange={setName}
                    placeholder="Speakeasy sign-in"
                    disabled={create.isPending}
                  />
                  <Text small muted>
                    Name it for what it’s for. Admins pick it from a list by
                    this name.
                  </Text>
                  {slug !== "" && slug !== name && !nameError && (
                    <Text small muted>
                      Saved as {slug}
                    </Text>
                  )}
                  {nameError && (
                    <Text small className="text-destructive">
                      {nameError}
                    </Text>
                  )}
                </Stack>

                <Stack gap={2}>
                  <Label>Session length</Label>
                  <Select
                    value={String(hours)}
                    onValueChange={(value) => setHours(Number(value))}
                    disabled={create.isPending}
                  >
                    <SelectTrigger
                      aria-label="Session length"
                      className="w-full"
                    >
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {SESSION_LENGTH_OPTIONS.map((option) => (
                        <SelectItem
                          key={option.hours}
                          value={String(option.hours)}
                        >
                          {option.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <Text small muted>
                    How long people stay signed in before they log in again.
                  </Text>
                </Stack>

                {organizationTier.isError && (
                  <Alert variant="error" dismissible={false}>
                    <Stack
                      direction="horizontal"
                      align="center"
                      justify="space-between"
                      gap={3}
                    >
                      <Text small>Couldn’t check your existing sign-ins.</Text>
                      <Button
                        variant="secondary"
                        size="sm"
                        disabled={organizationTier.isFetching}
                        onClick={organizationTier.retry}
                      >
                        <Button.Text>Retry</Button.Text>
                      </Button>
                    </Stack>
                  </Alert>
                )}

                {tierReady && hasExisting && (
                  <Alert variant="warning" dismissible={false}>
                    <Stack gap={3}>
                      <Text small>
                        {existingNames}{" "}
                        {organizationTier.items.length === 1 ? "is" : "are"}{" "}
                        already organization-wide. With two, the organization
                        gateway is off until only one is left.
                      </Text>
                      <label className="flex items-center gap-2">
                        <Checkbox
                          checked={acknowledged}
                          onCheckedChange={(checked) =>
                            setAcknowledged(checked === true)
                          }
                          disabled={create.isPending}
                        />
                        <Text small as="span">
                          Turn off the organization gateway
                        </Text>
                      </label>
                    </Stack>
                  </Alert>
                )}

                {create.isError && (
                  <Alert variant="error" dismissible={false}>
                    Couldn’t create the sign-in. Try again.
                  </Alert>
                )}
              </>
            )}
          </Stack>
        </div>

        <SheetFooter className="flex-row items-center justify-end gap-2 border-t px-6 py-4">
          <Button
            variant="secondary"
            disabled={create.isPending}
            onClick={() => onOpenChange(false)}
          >
            <Button.Text>Cancel</Button.Text>
          </Button>
          {method === "okta" ? (
            <Button asChild>
              <Link to={oktaSetupHref}>Go to Okta sign-in</Link>
            </Button>
          ) : (
            <Button
              variant="primary"
              disabled={!canCreate}
              onClick={() =>
                create.mutate({
                  request: {
                    createOrganizationUserSessionIssuerForm: {
                      slug,
                      authnChallengeMode: "interactive",
                      sessionDurationHours: hours,
                    },
                  },
                })
              }
            >
              <Button.Text>
                {create.isPending ? "Creating…" : "Create"}
              </Button.Text>
            </Button>
          )}
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}
