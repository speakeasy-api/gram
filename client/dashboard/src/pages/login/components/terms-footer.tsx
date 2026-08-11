import { cn } from "@/lib/utils";

// Legal footer shared by every auth surface (login/register shell and the
// demo-gate AuthLayout) so the links and copy stay in one place.
export function TermsFooter({
  className,
  linkClassName,
}: {
  className?: string;
  linkClassName?: string;
}): JSX.Element {
  return (
    <p className={cn("text-center text-[12px]", className)}>
      By continuing, you agree to Speakeasy&apos;s{" "}
      <a
        href="https://www.speakeasy.com/terms-of-service"
        target="_blank"
        rel="noopener noreferrer"
        className={cn("underline", linkClassName)}
      >
        Terms of Service
      </a>{" "}
      and{" "}
      <a
        href="https://www.speakeasy.com/privacy-policy"
        target="_blank"
        rel="noopener noreferrer"
        className={cn("underline", linkClassName)}
      >
        Privacy Policy
      </a>
    </p>
  );
}
