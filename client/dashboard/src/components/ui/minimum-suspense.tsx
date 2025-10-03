import { Suspense, useEffect, useState } from "react";

// Custom Suspense component with minimum loading time
// This is used to ensure the loader is visible for a minimum amount of time to avoid flickering
export const MinimumSuspense = ({
  children,
  fallback,
  minimumLoadTimeMs = 750,
}: {
  children: React.ReactNode;
  fallback: React.ReactNode;
  minimumLoadTimeMs?: number;
}) => {
  const [isLoading, setIsLoading] = useState(true);
  const [showFallback, setShowFallback] = useState(false);

  useEffect(() => {
    if (!showFallback) {
      return;
    }

    setIsLoading(true);
    const timer = setTimeout(() => {
      setIsLoading(false);
    }, minimumLoadTimeMs);

    return () => clearTimeout(timer);
  }, [minimumLoadTimeMs, showFallback]);

  // This is used to ensure the timer gets reset every time the fallback is shown
  const FallbackHandler = () => {
    useEffect(() => {
      setShowFallback(true);
      return () => setShowFallback(false);
    }, []);
    return <>{fallback}</>;
  };

  return (
    <Suspense fallback={<FallbackHandler />}>
      {isLoading ? <NeverResolves /> : children}
    </Suspense>
  );
};

// Component that never resolves during the minimum loading time
const NeverResolves = () => {
  throw new Promise(() => {});
};
