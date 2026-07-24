import { buildLoginRedirectURL } from "@/lib/utils";
import { useSearchParams } from "react-router";
import { getAuthErrorMessage } from "./auth-errors";
import { BrandLockup } from "./auth-shell";

const PILLARS = ["Observe", "Secure", "Connect", "Distribute"];

export function LoginPanel({
  redirectTo,
}: {
  redirectTo: string | null;
}): JSX.Element {
  const [searchParams] = useSearchParams();
  const signinError = searchParams.get("signin_error");

  const handleLogin = () => {
    window.location.href = buildLoginRedirectURL(redirectTo);
  };

  return (
    <>
      <BrandLockup />

      <div className="text-center">
        <p className="text-[16px]">
          Securely scale AI usage across your organization.
        </p>
        <p className="mt-1.5 text-[14px] text-[var(--stone)]">
          Control plane to govern Agents, MCP and Skills.
        </p>
      </div>

      <div className="flex gap-2">
        {PILLARS.map((label) => (
          <span
            key={label}
            className="auth-mono rounded-full border border-[var(--rule)] px-[11px] py-[5px] text-[11px]"
          >
            {label}
          </span>
        ))}
      </div>

      {signinError && (
        <p className="text-center text-[14px] text-[var(--vermilion)]">
          {getAuthErrorMessage(signinError)}
        </p>
      )}

      <button
        onClick={handleLogin}
        className="mt-2 w-[280px] bg-[var(--ink)] py-3.5 text-center text-[16px] text-[var(--bone)] transition-opacity hover:opacity-85"
      >
        Log in
      </button>

      <p className="auth-mono-text text-center text-[11px] leading-relaxed tracking-[0.02em] text-[var(--stone)]">
        Single sign-on through your identity provider.
      </p>
    </>
  );
}
