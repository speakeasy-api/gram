"use client";

import { Button } from "@speakeasy-api/moonshine";
import { buildLoginRedirectURL } from "@/lib/utils";
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
          className="rounded-full border border-[#D3D3D3] px-3 py-1 font-mono text-xs tracking-[0.01em] text-[#8B8684] uppercase"
        >
          {label}
        </span>
      ))}
    </div>
  );
}

// Full-spectrum RGB gradient — Speakeasy brand signature element
function BrandGradientBar() {
  return (
    <div
      className="absolute right-0 bottom-0 left-0 h-[6px]"
      style={{
        background:
          "linear-gradient(90deg, #320F1E 0%, #C83228 12.5%, #FB873F 25%, #D2DC91 37.5%, #5A8250 50%, #002314 62%, #00143C 74%, #2873D7 86%, #9BC3FF 100%)",
      }}
    />
  );
}

// Moving dot background — same pattern as MCP cards, scrolls on hover
function DotBackground() {
  return (
    <>
      <style>{`
        @keyframes login-right-scroll-dots {
          from { background-position: 0 0; }
          to { background-position: 64px 64px; }
        }
        .login-right-pane:hover .login-right-dots {
          animation: login-right-scroll-dots 3s linear infinite;
        }
      `}</style>
      <div
        className="login-right-dots text-muted-foreground/10 pointer-events-none absolute inset-0"
        style={{
          backgroundImage:
            "radial-gradient(circle, currentColor 1px, transparent 1px)",
          backgroundSize: "16px 16px",
        }}
      />
    </>
  );
}

function AuthLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="login-right-pane relative flex min-h-screen w-full flex-col items-center justify-center overflow-hidden bg-[#FAFAFA] p-8 md:w-1/2 md:p-16">
      {/* Moving dot background — scrolls on hover */}
      <DotBackground />

      <div className="relative z-10 flex w-full max-w-sm flex-col items-center gap-8">
        <div className="flex flex-col items-center gap-4">
          <a
            href="https://www.speakeasy.com/product/mcp-platform"
            target="_blank"
            rel="noopener noreferrer"
          >
            <GramLogo
              className="mb-2 w-[200px] dark:!invert-0"
              variant="vertical"
            />
          </a>
          <div className="flex flex-col gap-2 text-center text-sm dark:text-black">
            <p>Securely scale AI usage across your organization.</p>
            <p className="text-[#8B8684]">
              Control plane for distribution of MCP, Skills, Assistants and
              more.
            </p>
          </div>
          <FeatureBadges />
        </div>

        {children}
      </div>

      <p className="absolute bottom-10 z-10 px-8 text-center text-[11px] text-[#8B8684]">
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
    window.location.href = buildLoginRedirectURL(redirectTo);
  };

  return (
    <AuthLayout>
      {signinError && (
        <p className="mb-4 text-center text-red-600">
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
        <p className="mb-4 text-center text-red-600">
          login error: {getAuthErrorMessage(signinError)}
        </p>
      )}

      <form onSubmit={handleSubmit} className="flex w-full flex-col gap-4">
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
            className="w-full rounded-md border border-gray-300 px-3 py-2 text-gray-900 placeholder-gray-500 focus:border-transparent focus:ring-2 focus:ring-blue-500 focus:outline-none"
            disabled={registerMutation.isPending}
          />
        </div>

        {(validationError || registerMutation.error) && (
          <p className="text-center text-sm text-red-600">
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
