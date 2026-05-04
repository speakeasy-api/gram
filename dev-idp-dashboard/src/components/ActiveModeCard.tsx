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
  let nameLabel = "no current user yet";
  let idLabel: string | null = null;
  if (currentUser?.user) {
    nameLabel = currentUser.user.display_name;
    idLabel = currentUser.user.id;
  } else if (currentUser?.workos) {
    const fullName = [
      currentUser.workos.first_name,
      currentUser.workos.last_name,
    ]
      .filter(Boolean)
      .join(" ");
    nameLabel =
      fullName || currentUser.workos.email || currentUser.workos.workos_sub;
    idLabel = currentUser.workos.workos_sub;
  }

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
                <div className="mt-2 text-sm flex items-center gap-2 flex-wrap">
                  <span className="text-muted-foreground">Current user:</span>
                  <span className="font-medium">{nameLabel}</span>
                  {idLabel && (
                    <code className="font-mono text-[10px] px-1.5 py-0.5 rounded bg-muted text-muted-foreground">
                      {idLabel}
                    </code>
                  )}
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
