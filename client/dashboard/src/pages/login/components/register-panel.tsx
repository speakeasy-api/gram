import { useTelemetry } from "@/contexts/Telemetry";
import { safeSameOriginUrl } from "@/lib/safe-external-url";
import { cn } from "@/lib/utils";
import { authInfo } from "@gram/client/funcs/authInfo";
import { useGramContext } from "@gram/client/react-query/_context.js";
import {
  buildRegisterMutation,
  RegisterMutationVariables,
} from "@gram/client/react-query/register.js";
import { useMutation } from "@tanstack/react-query";
import { useState } from "react";
import { AUTH_BUTTON_CLASSES } from "./auth-constants";
import { AuthErrorText, SigninErrorNotice } from "./auth-errors";
import {
  MAX_ORG_NAME_LENGTH,
  normalizeOrgName,
  validateOrgName,
} from "./org-name";

export function RegisterPanel({
  redirectTo,
  partnerCode: initialPartnerCode = "",
}: {
  redirectTo?: string | null;
  partnerCode?: string;
}): JSX.Element {
  const telemetry = useTelemetry();
  const [partnerCode] = useState(initialPartnerCode);
  const [companyName, setCompanyName] = useState("");
  const [validationError, setValidationError] = useState("");
  const sdk = useGramContext();

  const registerMutation = useMutation({
    mutationFn: async (vars: RegisterMutationVariables) => {
      await buildRegisterMutation(sdk).mutationFn(vars);

      const info = await authInfo(sdk);
      if (!info.ok) {
        throw info.error;
      }

      const org = info.value.result.organizations.find(
        (org) => org.id === info.value.result.activeOrganizationId,
      );
      if (!org) {
        throw new Error("Organization not found");
      }

      return org;
    },

    onSuccess: (org, variables) => {
      telemetry.capture("onboarding_event", {
        action: "new_org_created",
        company_name: companyName,
        is_gram: true,
      });
      const destination =
        variables.request.registerRequestBody.partnerCode?.trim()
          ? `/${org.slug}/billing`
          : (safeSameOriginUrl(redirectTo) ?? "/");
      window.location.replace(destination);
    },
  });

  const handleCompanyNameChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const value = e.target.value;
    setCompanyName(value);

    // An empty field is the pristine state, not an error: the CTA is disabled
    // until something is typed, and submitting reports it as required.
    setValidationError(value.trim() ? (validateOrgName(value) ?? "") : "");
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();

    if (partnerCode.length > 80) return;
    const error = validateOrgName(companyName);
    if (error) {
      setValidationError(error);
      return;
    }

    const submittedPartnerCode = partnerCode.trim();
    telemetry.capture("onboarding_event", {
      action: "signup_started",
      ...(submittedPartnerCode ? { partner_code: submittedPartnerCode } : {}),
    });

    registerMutation.mutate({
      request: {
        registerRequestBody: {
          orgName: normalizeOrgName(companyName),
          partnerCode: submittedPartnerCode || undefined,
        },
      },
    });
  };

  return (
    <>
      <div className="text-center tracking-[0.0025em]">
        <p className="text-[16px]">Create your organization.</p>
        <p className="mt-1.5 text-[14px] text-[var(--muted-strong)]">
          Name your workspace — you can invite your team next.
        </p>
      </div>

      <SigninErrorNotice />

      <form
        onSubmit={handleSubmit}
        className="mt-2 flex w-full flex-col items-center gap-6"
      >
        <div className="flex w-full flex-col gap-2.5">
          <label
            htmlFor="companyName"
            className="auth-mono text-[12px] text-[var(--muted-strong)]"
          >
            Company name
          </label>
          <input
            id="companyName"
            type="text"
            value={companyName}
            onChange={handleCompanyNameChange}
            placeholder="Acme Inc"
            className="w-full border border-[var(--input-edge)] bg-[var(--card)] px-3.5 py-[11px] text-[16px] text-black placeholder:text-[var(--muted)] placeholder:opacity-55 focus:border-[var(--focus)] focus:outline-none"
            disabled={registerMutation.isPending}
          />
          <p className="text-[12px] text-[var(--muted)]">
            Any language, up to {MAX_ORG_NAME_LENGTH} characters.
          </p>
        </div>

        {partnerCode.length > 80 && (
          <AuthErrorText>
            Partner code must be 80 characters or fewer.
          </AuthErrorText>
        )}

        {(validationError || registerMutation.error) && (
          <AuthErrorText>
            {validationError || registerMutation.error?.message}
          </AuthErrorText>
        )}

        <button
          type="submit"
          disabled={
            registerMutation.isPending ||
            !companyName.trim() ||
            partnerCode.length > 80 ||
            Boolean(validationError)
          }
          className={cn(
            AUTH_BUTTON_CLASSES,
            // Deliberate disabled treatment: muted fill and text instead of
            // dimming the solid-ink CTA with opacity.
            "w-full disabled:cursor-not-allowed disabled:bg-[var(--edge)] disabled:text-[var(--muted)] disabled:hover:bg-[var(--edge)]",
          )}
        >
          Create organization
        </button>
      </form>
    </>
  );
}
