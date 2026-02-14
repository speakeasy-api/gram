import { useAuth } from "@/contexts/AuthContext";

interface ChatHeaderProps {
  chatName?: string;
}

export function ChatHeader({ chatName }: ChatHeaderProps) {
  const { user } = useAuth();

  return (
    <header className="flex h-14 shrink-0 items-center justify-between border-b border-neutral-800 bg-neutral-950 px-4">
      <div className="flex items-center gap-3">
        <svg
          width="28"
          height="28"
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
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium text-white">Gram Chat</span>
          {chatName && (
            <>
              <span className="text-neutral-600">/</span>
              <span className="text-sm text-neutral-400">{chatName}</span>
            </>
          )}
        </div>
      </div>

      <div className="flex items-center gap-3">
        {user && (
          <div className="flex items-center gap-2">
            {user.photoUrl ? (
              <img
                src={user.photoUrl}
                alt={user.displayName || user.email}
                className="h-7 w-7 rounded-full"
              />
            ) : (
              <div className="flex h-7 w-7 items-center justify-center rounded-full bg-neutral-700 text-xs font-medium text-white">
                {(user.displayName || user.email).charAt(0).toUpperCase()}
              </div>
            )}
            <span className="text-sm text-neutral-400">
              {user.displayName || user.email}
            </span>
          </div>
        )}
      </div>
    </header>
  );
}
