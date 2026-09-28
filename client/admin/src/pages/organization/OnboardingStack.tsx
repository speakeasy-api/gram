import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState, type JSX } from "react";
import type { AdminOnboardingStack } from "@gram/admin-client/models/components/adminonboardingstack";
import type { AdminOnboardingStackOptions } from "@gram/admin-client/models/components/adminonboardingstackoptions";
import type { SetOrganizationOnboardingStackRequestBody } from "@gram/admin-client/models/components/setorganizationonboardingstackrequestbody";

import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { errorMessage } from "@/lib/gramAdminApi";
import {
  onboardingStackOptionsQuery,
  organizationOnboardingStackQuery,
  setAdminOrganizationOnboardingStack,
} from "@/lib/gramAdminClient";

/** The server's answer for "no device management". */
const NO_MDM = "none";
/** The server's answer for software the form does not list. */
const OTHER_MDM = "other";

/**
 * What the form holds while staff edit. A selected vendor maps to the plan
 * chosen for it, or null while none is chosen yet.
 */
interface Draft {
  vendors: Record<string, string | null>;
  mdm: boolean;
  mdmVendor: string;
  mdmVendorName: string;
}

function draftFromStack(stack: AdminOnboardingStack): Draft {
  const vendors: Record<string, string | null> = {};
  for (const vendor of stack.vendors) {
    vendors[vendor.vendor] = vendor.planSlug ?? null;
  }
  const mdm = stack.mdmVendor !== undefined && stack.mdmVendor !== NO_MDM;
  return {
    vendors,
    mdm,
    mdmVendor: mdm ? (stack.mdmVendor ?? "") : "",
    mdmVendorName: stack.mdmVendorName ?? "",
  };
}

function toRequest(
  organizationId: string,
  draft: Draft,
  options: AdminOnboardingStackOptions,
): SetOrganizationOnboardingStackRequestBody {
  const other = draft.mdm && draft.mdmVendor === OTHER_MDM;
  return {
    organizationId,
    // Catalog order, so the request reads like the form.
    vendors: options.vendors
      .filter((option) => Object.hasOwn(draft.vendors, option.vendor))
      .map((option) => ({
        vendor: option.vendor,
        planSlug: draft.vendors[option.vendor] ?? undefined,
      })),
    mdmVendor: draft.mdm ? draft.mdmVendor : NO_MDM,
    mdmVendorName: other ? draft.mdmVendorName.trim() : undefined,
  };
}

/** The first thing the server would reject, in the form's own words. */
function draftProblem(
  draft: Draft,
  options: AdminOnboardingStackOptions,
): string | null {
  for (const option of options.vendors) {
    const selected = Object.hasOwn(draft.vendors, option.vendor);
    if (selected && option.plans.length > 0 && !draft.vendors[option.vendor]) {
      return `Choose the plan for ${option.vendor}.`;
    }
  }
  if (draft.mdm && !draft.mdmVendor) {
    return "Choose the device management software.";
  }
  if (
    draft.mdm &&
    draft.mdmVendor === OTHER_MDM &&
    !draft.mdmVendorName.trim()
  ) {
    return "Name the device management software.";
  }
  return null;
}

/**
 * The stack form, framed by its host: the organization Overview page puts it
 * in a panel with its heading.
 */
export function OnboardingStack({
  organizationId,
}: {
  organizationId: string;
}): JSX.Element {
  return (
    <OnboardingStackEditor
      key={organizationId}
      organizationId={organizationId}
    />
  );
}

function OnboardingStackEditor({
  organizationId,
}: {
  organizationId: string;
}): JSX.Element {
  const queryClient = useQueryClient();
  const stackQuery = organizationOnboardingStackQuery(organizationId);
  const options = useQuery({
    ...onboardingStackOptionsQuery(),
    throwOnError: false,
  });
  const stack = useQuery({ ...stackQuery, throwOnError: false });
  const [draft, setDraft] = useState<Draft | null>(null);
  const mutation = useMutation({
    mutationFn: setAdminOrganizationOnboardingStack,
    onSuccess: async (updated) => {
      await queryClient.cancelQueries({ queryKey: stackQuery.queryKey });
      queryClient.setQueryData(stackQuery.queryKey, updated);
      setDraft(null);
      await queryClient.invalidateQueries({ queryKey: stackQuery.queryKey });
    },
  });

  if (options.isPending || stack.isPending) {
    return <p role="status">Loading stack...</p>;
  }
  if (!options.data || !stack.data) {
    return (
      <div role="alert">
        <p>Unable to load the stack.</p>
        <Button
          variant="outline"
          onClick={() => {
            void options.refetch();
            void stack.refetch();
          }}
        >
          Retry stack
        </Button>
      </div>
    );
  }

  const catalog = options.data;
  const saved = stack.data;
  const current = draft ?? draftFromStack(saved);
  const dirty = draft !== null;
  const problem = draftProblem(current, catalog);
  const edit = (next: Draft) => {
    mutation.reset();
    setDraft(next);
  };

  return (
    <div className="space-y-5">
      <p className="text-muted-foreground text-sm">
        The vendors the organization uses, the plan it is on with each, and its
        device management. Every product of a vendor is implied. The lists come
        from the support matrix.
      </p>
      <fieldset className="space-y-3">
        <legend className="text-sm font-medium">Vendors</legend>
        {catalog.vendors.map((option) => {
          const selected = Object.hasOwn(current.vendors, option.vendor);
          return (
            <div key={option.vendor} className="space-y-2">
              <label className="flex cursor-pointer items-center gap-3">
                <Checkbox
                  aria-label={option.vendor}
                  checked={selected}
                  disabled={mutation.isPending}
                  onCheckedChange={(checked) => {
                    const vendors = { ...current.vendors };
                    if (checked === true) {
                      vendors[option.vendor] =
                        option.plans.length === 1
                          ? (option.plans[0]?.slug ?? null)
                          : null;
                    } else {
                      delete vendors[option.vendor];
                    }
                    edit({ ...current, vendors });
                  }}
                />
                <span className="text-sm font-medium">{option.vendor}</span>
              </label>
              {selected && option.plans.length > 0 ? (
                <div className="ml-7">
                  <Select
                    value={current.vendors[option.vendor] ?? ""}
                    onValueChange={(plan) =>
                      edit({
                        ...current,
                        vendors: {
                          ...current.vendors,
                          [option.vendor]: plan,
                        },
                      })
                    }
                    disabled={mutation.isPending}
                  >
                    <SelectTrigger
                      aria-label={`${option.vendor} plan`}
                      className="w-64"
                    >
                      <SelectValue placeholder="Choose a plan" />
                    </SelectTrigger>
                    <SelectContent>
                      {option.plans.map((plan) => (
                        <SelectItem key={plan.slug} value={plan.slug}>
                          {plan.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              ) : null}
            </div>
          );
        })}
      </fieldset>
      <fieldset className="space-y-3">
        <legend className="text-sm font-medium">Device management</legend>
        <label className="flex cursor-pointer items-center gap-3">
          <Checkbox
            aria-label="Uses device management software"
            checked={current.mdm}
            disabled={mutation.isPending}
            onCheckedChange={(checked) =>
              edit({
                ...current,
                mdm: checked === true,
                mdmVendor: checked === true ? current.mdmVendor : "",
              })
            }
          />
          <span className="text-sm">Uses device management software</span>
        </label>
        {current.mdm ? (
          <div className="ml-7 space-y-2">
            <Select
              value={current.mdmVendor}
              onValueChange={(vendor) =>
                edit({ ...current, mdmVendor: vendor })
              }
              disabled={mutation.isPending}
            >
              <SelectTrigger
                aria-label="Device management software"
                className="w-64"
              >
                <SelectValue placeholder="Choose the software" />
              </SelectTrigger>
              <SelectContent>
                {catalog.mdmVendors.map((vendor) => (
                  <SelectItem key={vendor.slug} value={vendor.slug}>
                    {vendor.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {current.mdmVendor === OTHER_MDM ? (
              <Input
                aria-label="Device management software name"
                placeholder="Software name"
                className="w-64"
                value={current.mdmVendorName}
                disabled={mutation.isPending}
                onChange={(event) =>
                  edit({ ...current, mdmVendorName: event.target.value })
                }
              />
            ) : null}
          </div>
        ) : null}
      </fieldset>
      <div className="flex flex-wrap items-center gap-2">
        <Button
          disabled={!dirty || problem !== null || mutation.isPending}
          onClick={() =>
            mutation.mutate(toRequest(organizationId, current, catalog))
          }
        >
          Save stack
        </Button>
        <Button
          variant="outline"
          disabled={!dirty || mutation.isPending}
          onClick={() => {
            mutation.reset();
            setDraft(null);
          }}
        >
          Discard
        </Button>
        {dirty && problem ? (
          <p className="text-muted-foreground text-sm">{problem}</p>
        ) : null}
      </div>
      {mutation.isError ? (
        <p role="alert" className="text-destructive text-sm">
          {errorMessage(mutation.error)}
        </p>
      ) : null}
      {mutation.isSuccess && !dirty ? (
        <p role="status" className="text-muted-foreground text-sm">
          Stack saved.
        </p>
      ) : null}
    </div>
  );
}
