import {
  createContext,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from "react";
import { useNavigate } from "react-router";

interface User {
  id: string;
  email: string;
  displayName?: string;
  photoUrl?: string;
}

interface Organization {
  id: string;
  name: string;
  slug: string;
}

interface AuthState {
  user: User | null;
  organization: Organization | null;
  session: string | null;
  isLoading: boolean;
  isAuthenticated: boolean;
}

const AuthContext = createContext<AuthState>({
  user: null,
  organization: null,
  session: null,
  isLoading: true,
  isAuthenticated: false,
});

export function useAuth() {
  return useContext(AuthContext);
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const navigate = useNavigate();
  const [state, setState] = useState<AuthState>({
    user: null,
    organization: null,
    session: null,
    isLoading: true,
    isAuthenticated: false,
  });

  useEffect(() => {
    async function checkAuth() {
      try {
        // Use relative URL - proxied through the chat domain's nginx/ingress
        const res = await fetch("/rpc/auth.info", {
          method: "GET",
          credentials: "include",
          headers: {
            "Content-Type": "application/json",
          },
        });

        if (!res.ok) {
          // Not authenticated - redirect to login
          const currentPath = window.location.pathname + window.location.search;
          navigate(`/login?redirect=${encodeURIComponent(currentPath)}`);
          return;
        }

        const data = await res.json();
        const org = data.organizations?.[0];

        setState({
          user: {
            id: data.userId,
            email: data.userEmail,
            displayName: data.displayName,
            photoUrl: data.photoUrl,
          },
          organization: org
            ? {
                id: org.id,
                name: org.name,
                slug: org.slug,
              }
            : null,
          session: data.sessionToken,
          isLoading: false,
          isAuthenticated: true,
        });
      } catch {
        const currentPath = window.location.pathname + window.location.search;
        navigate(`/login?redirect=${encodeURIComponent(currentPath)}`);
      }
    }

    checkAuth();
  }, [navigate]);

  if (state.isLoading) {
    return (
      <div className="flex h-full items-center justify-center bg-neutral-950">
        <div className="text-neutral-400">Loading...</div>
      </div>
    );
  }

  return <AuthContext.Provider value={state}>{children}</AuthContext.Provider>;
}
