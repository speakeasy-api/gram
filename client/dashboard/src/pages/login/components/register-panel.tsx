import { useTelemetry } from "@/contexts/Telemetry";
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

const VALID_ORG_NAME_REGEX = /^[a-zA-Z0-9\s-_]+$/;
const INVALID_ORG_NAME_MESSAGE =
  "Company name contains invalid characters. Only letters, numbers, spaces, hyphens, and underscores are allowed.";

export function RegisterPanel(): JSX.Element {
  const telemetry = useTelemetry();
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

    onSuccess: () => {
      telemetry.capture("onboarding_event", {
        action: "new_org_created",
        company_name: companyName,
        is_gram: true,
      });
      window.location.replace("/");
    },
    onError: (error) => {
      setValidationError(error.message);
    },
  });

  const handleCompanyNameChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const value = e.target.value;
    setCompanyName(value);

    // Clear previous errors
    setValidationError("");

    // Validate using the regex on type
    if (value.trim() && !VALID_ORG_NAME_REGEX.test(value)) {
      setValidationError(INVALID_ORG_NAME_MESSAGE);
    }
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();

    if (!companyName.trim()) {
      setValidationError("Company name is required");
      return;
    }

    if (!VALID_ORG_NAME_REGEX.test(companyName)) {
      setValidationError(INVALID_ORG_NAME_MESSAGE);
      return;
    }

    // Call the register mutation
    registerMutation.mutate({
      request: {
        registerRequestBody: {
          orgName: companyName.trim(),
        },
      },
    });
  };

  return (
    <>
      <div className="text-center">
        <p className="text-[17px]">Create your organization.</p>
        <p className="mt-1.5 text-[15px] text-[var(--stone)]">
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
            className="auth-mono text-[12px] text-[var(--stone)]"
          >
            Company name
          </label>
          <input
            id="companyName"
            type="text"
            value={companyName}
            onChange={handleCompanyNameChange}
            placeholder="Acme Inc"
            className="w-full rounded-none border border-[var(--rule)] bg-[var(--paper)] px-3.5 py-[13px] text-[16px] text-[var(--ink)] placeholder:text-[var(--stone)] placeholder:opacity-55 focus:border-[var(--ink)] focus:outline-none"
            disabled={registerMutation.isPending}
          />
          <p className="text-[12px] text-[var(--stone)]">
            Letters, numbers, spaces, hyphens, and underscores.
          </p>
        </div>

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
            Boolean(validationError)
          }
          className={cn(
            AUTH_BUTTON_CLASSES,
            "w-full disabled:cursor-not-allowed disabled:opacity-50",
          )}
        >
          Create organization
        </button>
      </form>
    </>
  );
}
