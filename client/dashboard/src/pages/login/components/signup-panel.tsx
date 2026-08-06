import googleMark from "@/assets/google-mark.svg";
import { useTelemetry } from "@/contexts/Telemetry";
import { buildLoginRedirectURL, cn } from "@/lib/utils";
import { useForm } from "@tanstack/react-form";
import { Loader2 } from "lucide-react";
import { Link } from "react-router";
import { AUTH_BUTTON_CLASSES, AUTH_PILLARS } from "./auth-constants";
import { SigninErrorNotice } from "./auth-errors";

// Mirrors the server's validOrgNameRegex, down to the literal space: the
// server rejects every other whitespace character, and normalizeCompanyName
// has already collapsed runs of whitespace to single spaces by the time this
// runs.
const VALID_ORG_NAME_REGEX = /^[a-zA-Z0-9 _-]+$/;
const INVALID_ORG_NAME_MESSAGE =
  "Company name contains invalid characters. Only letters, numbers, spaces, hyphens, and underscores are allowed.";

// Matches the server's cap in validateOrgName. The server is authoritative and
// rejects anything longer before the identity-provider hop; this only saves the
// round trip.
const MAX_ORG_NAME_LENGTH = 100;

// Pasted names often carry a non-breaking space, which JavaScript's `\s`
// accepts and the server's Go regex does not.
function normalizeCompanyName(value: string): string {
  return value.replace(/\s+/g, " ").trim();
}

function validateCompanyName(value: string): string | undefined {
  const normalized = normalizeCompanyName(value);
  if (!normalized) return "Company name is required";
  if (normalized.length > MAX_ORG_NAME_LENGTH) {
    return `Company name must be ${MAX_ORG_NAME_LENGTH} characters or fewer`;
  }
  return VALID_ORG_NAME_REGEX.test(normalized)
    ? undefined
    : INVALID_ORG_NAME_MESSAGE;
}

/** First error string for a field, or undefined. */
function firstError(errors: unknown[]): string | undefined {
  const error = errors.find(Boolean);
  return typeof error === "string" ? error : undefined;
}

export function SignUpPanel(): JSX.Element {
  const telemetry = useTelemetry();
  const form = useForm({
    defaultValues: { companyName: "" },
    onSubmit: ({ value }) => {
      // The server has no identity until the identity provider answers, so it
      // can't count this attempt — only the client sees it. Firing here joins
      // signup_started to the server's new_org_created for a real conversion rate.
      telemetry.capture("onboarding_event", {
        action: "signup_started",
        created_via: "signup",
      });

      // The org itself is created server-side during the auth callback. The
      // name rides on this one login request as a query param, is stashed
      // against the login nonce, and stops there: it is not carried through
      // the identity-provider round trip and is not on the URL the user lands
      // on afterwards.
      window.location.assign(
        buildLoginRedirectURL(null, normalizeCompanyName(value.companyName)),
      );
    },
  });

  return (
    <>
      <div className="text-center tracking-[0.0025em]">
        <p className="text-[16px]">
          Securely scale AI usage across your organization.
        </p>
        <p className="mt-1.5 text-[14px] text-(--muted-strong)">
          Control plane to govern Agents, MCP and Skills
        </p>
      </div>

      <div className="flex gap-2">
        {AUTH_PILLARS.map((label) => (
          <span
            key={label}
            className="auth-mono rounded-full border border-(--edge) px-[11px] py-[5px] text-[11px]"
          >
            {label}
          </span>
        ))}
      </div>

      <form
        onSubmit={(e) => {
          e.preventDefault();
          void form.handleSubmit();
        }}
        className="mt-4 flex w-full flex-col gap-4"
      >
        <div className="flex flex-col gap-1">
          <p className="auth-mono text-[16px] tracking-[0.06em]">Sign up</p>
          <p className="auth-mono-text text-[12px] text-(--muted-strong)">
            Get a 14-day trial of the full control plane.
          </p>
        </div>

        {/* Field and CTA share one subscription: `form.state` is a plain getter
            onto the store, so only `Subscribe` (and `field.state` inside
            `Field`) re-renders when form state changes.

            Validators run on change and again on submit, so an untouched empty
            form starts error-free with the CTA enabled — the design's default
            state — and submitting it surfaces the required error rather than
            presenting a dead button. */}
        <form.Subscribe
          selector={(s) =>
            [s.canSubmit, s.isSubmitting, s.isSubmitted] as const
          }
        >
          {([canSubmit, isSubmitting, isSubmitted]) => (
            <>
              <form.Field
                name="companyName"
                validators={{
                  onChange: ({ value }) => validateCompanyName(value),
                  onSubmit: ({ value }) => validateCompanyName(value),
                }}
              >
                {(field) => {
                  const error = firstError(field.state.meta.errors);
                  return (
                    <div className="flex flex-col gap-1">
                      <label
                        htmlFor={field.name}
                        className="auth-mono text-[12px] text-(--muted-strong)"
                      >
                        Company name
                      </label>
                      <input
                        id={field.name}
                        name={field.name}
                        type="text"
                        value={field.state.value}
                        onChange={(e) => field.handleChange(e.target.value)}
                        onBlur={field.handleBlur}
                        placeholder="Acme Inc"
                        disabled={isSubmitting}
                        aria-invalid={error ? true : undefined}
                        aria-describedby={
                          error ? `${field.name}-error` : undefined
                        }
                        className={cn(
                          "w-full rounded-md border bg-(--card) px-3.5 py-[11px] text-[16px] text-black placeholder:text-(--muted) placeholder:opacity-55 focus:outline-none",
                          error
                            ? "border-destructive-default"
                            : "border-(--input-edge) focus:border-(--focus)",
                        )}
                      />
                      {error && (
                        <p
                          id={`${field.name}-error`}
                          className="mt-0.5 text-[12px] leading-[1.45] text-destructive"
                        >
                          {error}
                        </p>
                      )}
                    </div>
                  );
                }}
              </form.Field>

              {/* The mark swaps for a spinner in the same leading slot and the
                  label is unchanged, so the button keeps its width while the
                  handoff is in flight.

                  isSubmitted keeps the button locked afterwards: the handoff is
                  a navigation, not an awaited promise, so isSubmitting flips
                  back before the browser leaves the page. Without it a second
                  click fires signup_started twice. */}
              <button
                type="submit"
                disabled={!canSubmit || isSubmitting || isSubmitted}
                className={cn(
                  AUTH_BUTTON_CLASSES,
                  "mx-auto gap-3 px-[22px] disabled:cursor-not-allowed disabled:opacity-50",
                )}
              >
                {isSubmitting ? (
                  <Loader2
                    className="h-[18px] w-[18px] flex-none animate-spin"
                    aria-hidden="true"
                  />
                ) : (
                  <img
                    src={googleMark}
                    alt=""
                    className="h-[18px] w-[18px] flex-none"
                  />
                )}
                Continue with Google
              </button>
            </>
          )}
        </form.Subscribe>
      </form>

      {/* Auth failures return as ?signin_error=. The design places the notice
          below the CTA, not above the heading. */}
      <SigninErrorNotice />

      <p className="mt-2 text-[14px] text-(--muted-strong)">
        Already have an account?{" "}
        <Link
          to="/login"
          className="text-(--link) underline hover:text-(--focus)"
        >
          Log in
        </Link>
      </p>
    </>
  );
}
