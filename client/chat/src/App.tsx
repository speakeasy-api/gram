import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter, Route, Routes } from "react-router";
import { AuthProvider } from "./contexts/AuthContext";
import { ChatPage } from "./pages/ChatPage";
import { LoginPage } from "./pages/LoginPage";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      staleTime: 30_000,
    },
  },
});

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route
            path="/:chatSlug"
            element={
              <AuthProvider>
                <ChatPage />
              </AuthProvider>
            }
          />
          <Route
            path="/"
            element={
              <AuthProvider>
                <LandingPage />
              </AuthProvider>
            }
          />
        </Routes>
      </BrowserRouter>
    </QueryClientProvider>
  );
}

function LandingPage() {
  return (
    <div className="flex h-full flex-col items-center justify-center bg-neutral-950 text-white">
      <GramLogo />
      <h1 className="mt-6 text-2xl font-semibold">Gram Chat</h1>
      <p className="mt-2 text-neutral-400">
        No chat instance specified. Navigate to a chat URL to get started.
      </p>
    </div>
  );
}

function GramLogo() {
  return (
    <svg
      width="48"
      height="48"
      viewBox="0 0 32 32"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
    >
      <rect width="32" height="32" rx="8" fill="#6366f1" />
      <text
        x="16"
        y="22"
        textAnchor="middle"
        fill="white"
        fontSize="18"
        fontWeight="bold"
        fontFamily="sans-serif"
      >
        G
      </text>
    </svg>
  );
}
