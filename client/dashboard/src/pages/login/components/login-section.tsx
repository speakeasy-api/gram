"use client";

import { GramLogo } from "@/components/gram-logo";
import { Button } from "@/components/ui/button";

export function LoginSection() {
  const handleLogin = async () => {
    window.location.href = "http://localhost:8080/rpc/auth.login";
  };

  return (
    <div className="flex flex-col justify-center items-center w-full md:w-1/2 min-h-screen p-8 md:p-16 bg-white">
      <div className="w-full max-w-md flex flex-col items-center">
        <div className="flex flex-col items-center text-center mb-12">
          <GramLogo className="text-7xl" />
        </div>

        <p className="text-gray-600 text-center max-w-sm text-[22px] tracking-wide font-light mb-16">
          Your AI-powered chat assistant for calling APIs with natural language
        </p>

        <Button
          className="w-80 py-6 text-lg text-white font-mono font-light tracking-wide uppercase"
          style={{
            background: "linear-gradient(2.77deg, #1E1E21 1.88%, #2B2B2F 97.5%)",
            border: "none",
            fontFamily: "var(--font-ibm-plex-mono)", // Add explicit font-family
          }}
          onClick={handleLogin}
        >
          Login
        </Button>
      </div>
    </div>
  );
}
