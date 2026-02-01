import { useSessionData } from "@/contexts/Auth";
import { GramLogo } from "@/components/gram-logo/index";
import { useAcceptTeamInviteMutation } from "@gram/client/react-query";
import { useEffect, useRef } from "react";
import { useSearchParams, useNavigate } from "react-router";
import { Loader2 } from "lucide-react";
import { Button } from "@speakeasy-api/moonshine";

export default function AcceptInvite() {
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const token = searchParams.get("token");

  const { session, error: sessionError, status } = useSessionData();
  const isLoading = status === "pending";
  const isAuthenticated = !!session?.session;

  const mutation = useAcceptTeamInviteMutation();
  const attempted = useRef(false);

  // Redirect to login if not authenticated (once session check is done)
  useEffect(() => {
    if (isLoading) return;
    if (!isAuthenticated) {
      const currentUrl = `/invite?token=${encodeURIComponent(token ?? "")}`;
      navigate(`/login?redirect=${encodeURIComponent(currentUrl)}`, {
        replace: true,
      });
    }
  }, [isLoading, isAuthenticated, token, navigate]);

  // Auto-accept once authenticated
  useEffect(() => {
    if (!isAuthenticated || !token || attempted.current) return;
    if (mutation.isPending || mutation.isSuccess || mutation.isError) return;

    attempted.current = true;
    mutation.mutate({
      request: {
        serveChatAttachmentSignedForm: { token },
      },
    });
  }, [isAuthenticated, token, mutation]);

  // Redirect on success
  useEffect(() => {
    if (mutation.isSuccess && mutation.data?.organizationSlug) {
      navigate(`/${mutation.data.organizationSlug}`, { replace: true });
    }
  }, [mutation.isSuccess, mutation.data, navigate]);

  if (!token) {
    return <InviteLayout message="Invalid invite link. No token provided." />;
  }

  if (isLoading) {
    return <InviteLayout loading message="Checking authentication..." />;
  }

  if (!isAuthenticated) {
    return <InviteLayout loading message="Redirecting to login..." />;
  }

  if (mutation.isPending) {
    return <InviteLayout loading message="Accepting invite..." />;
  }

  if (mutation.isError) {
    const errorMessage = getErrorMessage(mutation.error);
    return (
      <InviteLayout message={errorMessage}>
        <Button
          variant="secondary"
          onPress={() => navigate("/", { replace: true })}
        >
          Go to dashboard
        </Button>
      </InviteLayout>
    );
  }

  if (mutation.isSuccess) {
    return <InviteLayout loading message="Invite accepted! Redirecting..." />;
  }

  return <InviteLayout loading message="Processing invite..." />;
}

function InviteLayout({
  message,
  loading,
  children,
}: {
  message: string;
  loading?: boolean;
  children?: React.ReactNode;
}) {
  return (
    <main className="flex min-h-screen items-center justify-center">
      <div className="flex flex-col items-center gap-6 text-center">
        <GramLogo />
        <div className="flex items-center gap-2">
          {loading && <Loader2 className="h-4 w-4 animate-spin" />}
          <p className="text-sm">{message}</p>
        </div>
        {children}
      </div>
    </main>
  );
}

function getErrorMessage(error: unknown): string {
  if (error && typeof error === "object" && "message" in error) {
    const msg = (error as { message: string }).message;
    if (msg.includes("expired")) return "This invite has expired.";
    if (msg.includes("no longer pending"))
      return "This invite has already been used.";
    if (msg.includes("different email"))
      return "This invite was sent to a different email address. Please log in with the correct account.";
    if (msg.includes("not found"))
      return "This invite was not found or has already been used.";
    return msg;
  }
  return "An unexpected error occurred while accepting the invite.";
}
