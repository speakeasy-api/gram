import {
  BrandMeshLayers,
  BRAND_MESH_SURFACE_CLASS,
} from "@/components/brand-mesh";
import { useOrganization } from "@/contexts/Auth";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { handleAPIError } from "@/lib/errors";
import { cn } from "@/lib/utils";
import { SetRemoteSessionAutoRefreshPolicyRequestBodyPolicy as RefreshPolicy } from "@gram/client/models/components/setremotesessionautorefreshpolicyrequestbody.js";
import {
  invalidateAllProductFeatures,
  useProductFeatures,
} from "@gram/client/react-query/productFeatures.js";
import { useSetRemoteSessionAutoRefreshPolicyMutation } from "@gram/client/react-query/setRemoteSessionAutoRefreshPolicy.js";
import { useQueryClient } from "@tanstack/react-query";
import { Check, RefreshCw } from "lucide-react";
import { toast } from "sonner";
import { useIsCurrentOrganization } from "@/hooks/useIsCurrentOrganization";

const POLICY_OPTIONS = [
  {
    value: RefreshPolicy.Disabled,
    label: "Disabled",
    description:
      "Show refresh as off and managed by your organization, and let inactive connections expire.",
  },
  {
    value: RefreshPolicy.UserControlled,
    label: "User controlled",
    description:
      "Show the setting on consent screens, default it on, and let each user opt out.",
  },
  {
    value: RefreshPolicy.Enforced,
    label: "Required",
    description:
      "Keep every eligible connection refreshed and prevent users from turning it off.",
  },
] as const;

function selectedPolicy(
  visible: boolean,
  enforced: boolean,
): (typeof POLICY_OPTIONS)[number]["value"] {
  if (enforced) return RefreshPolicy.Enforced;
  if (visible) return RefreshPolicy.UserControlled;
  return RefreshPolicy.Disabled;
}

export function RemoteSessionRefreshPolicySetting(): JSX.Element {
  const organization = useOrganization();
  const isCurrentOrganization = useIsCurrentOrganization(organization.id);
  return (
    <RemoteSessionRefreshPolicySettingInner
      key={organization.id}
      organizationId={organization.id}
      isCurrentOrganization={isCurrentOrganization}
    />
  );
}

function RemoteSessionRefreshPolicySettingInner({
  organizationId,
  isCurrentOrganization,
}: {
  organizationId: string;
  isCurrentOrganization: () => boolean;
}): JSX.Element {
  const queryClient = useQueryClient();
  const features = useProductFeatures({ organizationId }, undefined, {
    throwOnError: false,
  });
  const mutation = useSetRemoteSessionAutoRefreshPolicyMutation({
    onSuccess: async () => {
      if (!isCurrentOrganization()) return;
      await invalidateAllProductFeatures(queryClient);
      if (!isCurrentOrganization()) return;
      toast.success("Automatic session refresh policy updated");
    },
    onError: (error) => {
      if (!isCurrentOrganization()) return;
      handleAPIError(
        error,
        "Failed to update automatic session refresh policy",
      );
    },
  });

  if (features.isLoading) {
    return <Skeleton className="h-44 w-full" />;
  }

  if (features.error || !features.data) {
    return (
      <section className="border-border bg-card border p-5">
        <Text className="text-destructive" small>
          Couldn&apos;t load the automatic session refresh policy.
        </Text>
        <Button
          variant="secondary"
          size="sm"
          className="mt-3"
          onClick={() => void features.refetch()}
        >
          Retry
        </Button>
      </section>
    );
  }

  const currentPolicy = selectedPolicy(
    features.data.remoteSessionAutoRefreshEnabled,
    features.data.remoteSessionAutoRefreshEnforcedEnabled,
  );

  return (
    // The same brand-mesh surface as the project home assistant card, composed
    // from the shared treatment rather than restated: a neutral theme-following
    // gradient with the rainbow breathing in from one edge and grain over the
    // top. No `overflow-hidden` — the decorative layers clip themselves.
    <section
      className={cn(BRAND_MESH_SURFACE_CLASS, "border-border border p-8")}
    >
      <BrandMeshLayers />

      <div className="mb-6 flex items-start gap-3">
        <div className="border-border bg-card flex size-9 shrink-0 items-center justify-center border">
          <RefreshCw className="text-muted-foreground size-4" />
        </div>
        <div>
          <Text variant="subheading">Automatic session refresh policy</Text>
          <Text small muted className="mt-1 max-w-3xl">
            Choose whether OAuth connections refresh before they expire and
            whether users can control that behavior on consent screens.
          </Text>
        </div>
      </div>

      <RequireScope
        scope="org:admin"
        level="component"
        className="w-full"
        reason="Only organization admins can change this policy."
      >
        {({ disabled }) => (
          <fieldset
            disabled={disabled || mutation.isPending}
            className="grid gap-3 md:grid-cols-3"
            aria-label="Automatic session refresh policy"
          >
            {POLICY_OPTIONS.map((option) => {
              const selected = currentPolicy === option.value;
              return (
                <label
                  key={option.value}
                  className={cn(
                    // Opaque cards, not transparent panels: they have to sit on
                    // the mesh rather than let it show through, or the option
                    // text competes with the gradient behind it.
                    "border-border bg-card focus-within:border-ring focus-within:ring-ring/50 flex min-h-28 cursor-pointer flex-col border p-4 transition-[color,box-shadow] focus-within:ring-[3px]",
                    selected && "border-foreground",
                    (disabled || mutation.isPending) &&
                      "cursor-not-allowed opacity-60",
                  )}
                >
                  <input
                    type="radio"
                    name="remote-session-refresh-policy"
                    value={option.value}
                    aria-label={option.label}
                    checked={selected}
                    onChange={() => {
                      if (disabled || mutation.isPending) return;
                      mutation.mutate({
                        request: {
                          setRemoteSessionAutoRefreshPolicyRequestBody: {
                            organizationId,
                            policy: option.value,
                          },
                        },
                      });
                    }}
                    className="sr-only"
                  />
                  <span className="flex items-center justify-between gap-2">
                    <Text className="font-medium">{option.label}</Text>
                    {selected ? (
                      <span className="bg-foreground text-background flex size-5 items-center justify-center">
                        <Check className="size-3.5" />
                      </span>
                    ) : null}
                  </span>
                  <Text small muted className="mt-2 leading-relaxed">
                    {option.description}
                  </Text>
                </label>
              );
            })}
          </fieldset>
        )}
      </RequireScope>
    </section>
  );
}
