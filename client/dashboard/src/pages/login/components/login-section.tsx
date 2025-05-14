"use client";

import { Button } from "@/components/ui/button";
import { getServerURL } from "@/lib/utils";
import { useSearchParams } from "react-router";

// TODO: FARAZ WILL FINISH THIS
export function LoginSection() {
  const [searchParams] = useSearchParams();
  const signinError = searchParams.get("signin_error");

  const handleLogin = async () => {
    window.location.href = `${getServerURL()}/rpc/auth.login`;
  };

  return (
    <div className="flex flex-col justify-center items-center w-full md:w-1/2 min-h-screen p-8 md:p-16 bg-white">
      <div className="w-full max-w-md flex flex-col items-center gap-8">
        <div className="flex flex-col items-center gap-4">
          <h1 className="text-heading-xl">gram</h1>
          <p className="text-body-lg text-center">
            The AI Tools platform, chat and Build agentic workflows with your
            APIs
          </p>
        </div>

        {signinError && (
          <p className="text-red-600 text-center mb-4">
            login error: {decodeURIComponent(signinError)}
          </p>
        )}

        <Button className="px-10 py-3 rounded-xs" onClick={handleLogin}>
          <span className="text-mono-light text-white">Login</span>
        </Button>
      </div>
    </div>
  );
}
