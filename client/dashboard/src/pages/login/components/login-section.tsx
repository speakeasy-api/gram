"use client";

import { Button } from "@speakeasy-api/moonshine";
import { cn, getServerURL } from "@/lib/utils";
import { useSearchParams } from "react-router";
import { useState } from "react";
import {
  buildRegisterMutation,
  RegisterMutationVariables,
  useGramContext,
} from "@gram/client/react-query";
import { authInfo } from "@gram/client/funcs/authInfo";
import { useTelemetry } from "@/contexts/Telemetry";
import { useMutation } from "@tanstack/react-query";
import { GramLogo } from "@/components/gram-logo/index";

const unexpected = "Server error. Please try again later or contact support.";
const authErrorMessages: Record<string, string> = {
  lookup_error: "Failed to look up account details.",
  init_error: "Failed to initialize account.",
  unexpected,
};

function getAuthErrorMessage(errorCode?: string | null): string {
  if (!errorCode) {
    return unexpected;
  }
  return authErrorMessages[errorCode] || unexpected;
}

const FEATURE_BADGES = ["Build", "Secure", "Observe", "Distribute"];

function FeatureBadges({ labels = FEATURE_BADGES }: { labels?: string[] }) {
  return (
    <div className="flex justify-center gap-2">
      {labels.map((label) => (
        <span
          key={label}
          className="rounded-full border border-[#D3D3D3] px-3 py-1 font-mono text-xs uppercase tracking-[0.01em] text-[#8B8684]"
        >
          {label}
        </span>
      ))}
    </div>
  );
}

function GradientButton({
  children,
  onClick,
  disabled,
  className,
  type = "button",
}: {
  children: React.ReactNode;
  onClick?: () => void;
  disabled?: boolean;
  className?: string;
  type?: "button" | "submit" | "reset";
}) {
  return (
    <button
      type={type}
      onClick={onClick}
      disabled={disabled}
      className={cn(
        "group relative inline-flex w-full cursor-pointer items-center justify-center rounded-full p-[2px] transition-all duration-200",
        "bg-gradient-to-br from-[#5A8250] via-[#2873D7] to-[#FB873F]",
        disabled && "pointer-events-none opacity-50",
        className,
      )}
    >
      <span
        className={cn(
          "flex w-full items-center justify-center rounded-full bg-white px-8 py-2 font-mono text-[15px] uppercase leading-[1.6] tracking-[0.01em] text-black transition-all duration-200",
          "group-hover:bg-transparent group-hover:text-white",
        )}
      >
        {children}
      </span>
    </button>
  );
}

// Full-spectrum RGB gradient — Speakeasy brand signature element
function BrandGradientBar() {
  return (
    <div
      className="absolute bottom-0 left-0 right-0 h-[6px]"
      style={{
        background:
          "linear-gradient(90deg, #320F1E 0%, #C83228 12.5%, #FB873F 25%, #D2DC91 37.5%, #5A8250 50%, #002314 62%, #00143C 74%, #2873D7 86%, #9BC3FF 100%)",
      }}
    />
  );
}

// Subtle grid lines for texture — brand book light layout pattern
function GridBackground() {
  return (
    <div
      className="absolute inset-0 pointer-events-none"
      style={{
        backgroundImage:
          "linear-gradient(to right, #ECECEC 1px, transparent 1px), linear-gradient(to bottom, #ECECEC 1px, transparent 1px)",
        backgroundSize: "48px 48px",
        opacity: 0.5,
      }}
    />
  );
}

function AuthLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex flex-col justify-center items-center w-full md:w-1/2 min-h-screen p-8 md:p-16 bg-[#FAFAFA] relative overflow-hidden">
      {/* Subtle grid texture */}
      <GridBackground />

      <div className="relative z-10 w-full flex flex-col items-center gap-8 max-w-sm">
        <div className="flex flex-col items-center gap-4">
          <a
            href="https://www.speakeasy.com/product/mcp-platform"
            target="_blank"
            rel="noopener noreferrer"
          >
            <GramLogo
              className="w-[200px] mb-2 dark:!invert-0"
              variant="vertical"
            />
          </a>
          <div className="flex flex-col gap-2 text-sm text-center dark:text-black">
            <p>
              Securely scale AI usage across your organisation
              with&nbsp;Speakeasy.
            </p>
            <p className="text-[#8B8684]">
              Control plane for distribution of MCP, Skills, CLIs and more.
            </p>
          </div>
          <FeatureBadges />
        </div>

        {children}
      </div>

      <p className="absolute bottom-10 z-10 text-[11px] text-[#8B8684] text-center px-8">
        By continuing, you agree to Speakeasy&apos;s{" "}
        <a
          href="https://www.speakeasy.com/terms-of-service"
          target="_blank"
          rel="noopener noreferrer"
          className="underline hover:text-slate-600"
        >
          Terms of Service
        </a>{" "}
        and{" "}
        <a
          href="https://www.speakeasy.com/privacy-policy"
          target="_blank"
          rel="noopener noreferrer"
          className="underline hover:text-slate-600"
        >
          Privacy Policy
        </a>
      </p>

      {/* Brand signature — RGB gradient bar at bottom edge */}
      <BrandGradientBar />
    </div>
  );
}

export type LoginSectionProps = {
  redirectTo: string | null;
};

export function LoginSection(props: LoginSectionProps) {
  const [searchParams] = useSearchParams();
  const signinError = searchParams.get("signin_error");

  const { redirectTo } = props;

  const handleLogin = async () => {
    let href = `${getServerURL()}/rpc/auth.login`;
    if (redirectTo) href += `?redirect=${encodeURIComponent(redirectTo)}`;

    window.location.href = href;
  };

  return (
    <AuthLayout>
      {signinError && (
        <p className="text-red-600 text-center mb-4">
          login error: {getAuthErrorMessage(signinError)}
        </p>
      )}

      <div className="relative z-10">
        <Button variant="brand" onClick={handleLogin}>
          Login
        </Button>
      </div>
    </AuthLayout>
  );
}

export function RegisterSection() {
  const [searchParams] = useSearchParams();
  const signinError = searchParams.get("signin_error");
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
    if (value.trim()) {
      const validOrgNameRegex = /^[a-zA-Z0-9\s-_]+$/;
      if (!validOrgNameRegex.test(value)) {
        setValidationError(
          "Company name contains invalid characters. Only letters, numbers, spaces, hyphens, and underscores are allowed.",
        );
      }
    }
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();

    if (!companyName.trim()) {
      setValidationError("Company name is required");
      return;
    }

    telemetry.capture("onboarding_event", {
      action: "new_org_created",
      company_name: companyName,
      is_gram: true,
    });

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
    <AuthLayout>
      {signinError && (
        <p className="text-red-600 text-center mb-4">
          login error: {getAuthErrorMessage(signinError)}
        </p>
      )}

      <form onSubmit={handleSubmit} className="w-full flex flex-col gap-4">
        <div className="flex flex-col gap-2">
          <label
            htmlFor="companyName"
            className="text-sm font-medium text-gray-700 dark:text-gray-800"
          >
            Company Name
          </label>
          <input
            id="companyName"
            type="text"
            value={companyName}
            onChange={handleCompanyNameChange}
            placeholder="company name"
            className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent placeholder-gray-500 text-gray-900"
            disabled={registerMutation.isPending}
          />
        </div>

        {(validationError || registerMutation.error) && (
          <p className="text-red-600 text-sm text-center">
            {validationError || registerMutation.error?.message}
          </p>
        )}

        <div className="relative z-10">
          <Button
            variant="brand"
            type="submit"
            disabled={registerMutation.isPending || !companyName.trim()}
            className="w-full"
          >
            Create Organization
          </Button>
        </div>
      </form>
    </AuthLayout>
  );
}
