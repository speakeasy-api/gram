import { Link } from "@tanstack/react-router";
import { motion } from "motion/react";
import { ArrowRight } from "lucide-react";
import { cn } from "@/lib/utils";
import { Card, CardContent } from "@/components/ui/card";
import { useGramMode } from "@/hooks/use-gram-mode";
import { MODE_LABELS, MODE_SUBTITLES } from "@/lib/mode-labels";

export function ActiveModeCard() {
  const { data, isLoading } = useGramMode();

  if (isLoading) {
    return (
      <Card size="sm" className="!rounded-md">
        <CardContent>
          <div className="h-12 animate-pulse" />
        </CardContent>
      </Card>
    );
  }

  if (!data?.mode) {
    return (
      <Card size="sm" className="!rounded-md">
        <CardContent>
          <div className="text-sm">
            <div className="font-medium">No active dev-idp mode detected</div>
            <p className="text-xs text-muted-foreground mt-1">
              Neither <code>SPEAKEASY_API_URL</code> nor{" "}
              <code>WORKOS_API_URL</code> points back at the dev-idp — Gram is
              configured against an external upstream.
            </p>
          </div>
        </CardContent>
      </Card>
    );
  }

  const { mode, currentUser } = data;
  const userLabel = currentUser?.user
    ? `${currentUser.user.display_name} <${currentUser.user.email}>`
    : currentUser?.workos
      ? `${currentUser.workos.email ?? currentUser.workos.workos_sub}`
      : "no current user yet";

  return (
    <motion.div layout>
      <Link to="/providers/$mode" params={{ mode }} className="block group">
        <Card
          size="sm"
          className={cn(
            "!rounded-md transition-colors",
            "group-hover:ring-2 group-hover:ring-[var(--retro-yellow)]/60",
          )}
        >
          <CardContent>
            <div className="flex items-center gap-4">
              <div className="min-w-0 flex-1">
                <div className="text-xs uppercase tracking-wider text-muted-foreground">
                  Gram is logging in via
                </div>
                <div className="font-semibold text-base font-mono">
                  {MODE_LABELS[mode]}
                </div>
                <div className="text-xs text-muted-foreground mt-0.5 truncate">
                  {MODE_SUBTITLES[mode]}
                </div>
                <div className="mt-2 text-sm">
                  <span className="text-muted-foreground">Current user:</span>{" "}
                  <span className="font-medium">{userLabel}</span>
                </div>
              </div>
              <ArrowRight className="text-muted-foreground group-hover:text-[var(--retro-orange)] transition-colors size-5 shrink-0" />
            </div>
          </CardContent>
        </Card>
      </Link>
    </motion.div>
  );
}
