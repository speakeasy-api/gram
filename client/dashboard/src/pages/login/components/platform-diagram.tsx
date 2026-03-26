import { GramLogo } from "@/components/gram-logo";
import { cn } from "@/lib/utils";
import { motion, useReducedMotion } from "motion/react";

// Brand gradient colors
const BRAND_COLORS = {
  green: "#5A8250",
  blue: "#2873D7",
  orange: "#FB873F",
};

// Service logo components (simplified SVG icons)
const GitHubLogo = () => (
  <svg viewBox="0 0 24 24" className="w-5 h-5" fill="currentColor">
    <path d="M12 0C5.37 0 0 5.37 0 12c0 5.31 3.435 9.795 8.205 11.385.6.105.825-.255.825-.57 0-.285-.015-1.23-.015-2.235-3.015.555-3.795-.735-4.035-1.41-.135-.345-.72-1.41-1.23-1.695-.42-.225-1.02-.78-.015-.795.945-.015 1.62.87 1.845 1.23 1.08 1.815 2.805 1.305 3.495.99.105-.78.42-1.305.765-1.605-2.67-.3-5.46-1.335-5.46-5.925 0-1.305.465-2.385 1.23-3.225-.12-.3-.54-1.53.12-3.18 0 0 1.005-.315 3.3 1.23.96-.27 1.98-.405 3-.405s2.04.135 3 .405c2.295-1.56 3.3-1.23 3.3-1.23.66 1.65.24 2.88.12 3.18.765.84 1.23 1.905 1.23 3.225 0 4.605-2.805 5.625-5.475 5.925.435.375.81 1.095.81 2.22 0 1.605-.015 2.895-.015 3.3 0 .315.225.69.825.57A12.02 12.02 0 0024 12c0-6.63-5.37-12-12-12z" />
  </svg>
);

const FigmaLogo = () => (
  <svg viewBox="0 0 24 24" className="w-5 h-5">
    <path
      d="M8 24c2.2 0 4-1.8 4-4v-4H8c-2.2 0-4 1.8-4 4s1.8 4 4 4z"
      fill="#0ACF83"
    />
    <path d="M4 12c0-2.2 1.8-4 4-4h4v8H8c-2.2 0-4-1.8-4-4z" fill="#A259FF" />
    <path d="M4 4c0-2.2 1.8-4 4-4h4v8H8C5.8 8 4 6.2 4 4z" fill="#F24E1E" />
    <path d="M12 0h4c2.2 0 4 1.8 4 4s-1.8 4-4 4h-4V0z" fill="#FF7262" />
    <path
      d="M20 12c0 2.2-1.8 4-4 4s-4-1.8-4-4 1.8-4 4-4 4 1.8 4 4z"
      fill="#1ABCFE"
    />
  </svg>
);

const SlackLogo = () => (
  <svg viewBox="0 0 24 24" className="w-5 h-5">
    <path
      d="M5.042 15.165a2.528 2.528 0 0 1-2.52 2.523A2.528 2.528 0 0 1 0 15.165a2.527 2.527 0 0 1 2.522-2.52h2.52v2.52zM6.313 15.165a2.527 2.527 0 0 1 2.521-2.52 2.527 2.527 0 0 1 2.521 2.52v6.313A2.528 2.528 0 0 1 8.834 24a2.528 2.528 0 0 1-2.521-2.522v-6.313zM8.834 5.042a2.528 2.528 0 0 1-2.521-2.52A2.528 2.528 0 0 1 8.834 0a2.528 2.528 0 0 1 2.521 2.522v2.52H8.834zM8.834 6.313a2.528 2.528 0 0 1 2.521 2.521 2.528 2.528 0 0 1-2.521 2.521H2.522A2.528 2.528 0 0 1 0 8.834a2.528 2.528 0 0 1 2.522-2.521h6.312zM18.956 8.834a2.528 2.528 0 0 1 2.522-2.521A2.528 2.528 0 0 1 24 8.834a2.528 2.528 0 0 1-2.522 2.521h-2.522V8.834zM17.688 8.834a2.528 2.528 0 0 1-2.523 2.521 2.527 2.527 0 0 1-2.52-2.521V2.522A2.527 2.527 0 0 1 15.165 0a2.528 2.528 0 0 1 2.523 2.522v6.312zM15.165 18.956a2.528 2.528 0 0 1 2.523 2.522A2.528 2.528 0 0 1 15.165 24a2.527 2.527 0 0 1-2.52-2.522v-2.522h2.52zM15.165 17.688a2.527 2.527 0 0 1-2.52-2.523 2.526 2.526 0 0 1 2.52-2.52h6.313A2.527 2.527 0 0 1 24 15.165a2.528 2.528 0 0 1-2.522 2.523h-6.313z"
      fill="#E01E5A"
    />
  </svg>
);

const NotionLogo = () => (
  <svg viewBox="0 0 24 24" className="w-5 h-5" fill="currentColor">
    <path d="M4.459 4.208c.746.606 1.026.56 2.428.466l13.215-.793c.28 0 .047-.28-.046-.326L17.86 1.968c-.42-.326-.98-.7-2.055-.607L3.01 2.295c-.466.046-.56.28-.374.466zm.793 3.08v13.904c0 .747.373 1.027 1.214.98l14.523-.84c.841-.046.935-.56.935-1.167V6.354c0-.606-.233-.933-.748-.886l-15.177.887c-.56.047-.747.327-.747.933zm14.337.745c.093.42 0 .84-.42.888l-.7.14v10.264c-.608.327-1.168.514-1.635.514-.748 0-.935-.234-1.495-.933l-4.577-7.186v6.952L12.21 19s0 .84-1.168.84l-3.222.186c-.093-.186 0-.653.327-.746l.84-.233V9.854L7.822 9.76c-.094-.42.14-1.026.793-1.073l3.456-.233 4.764 7.279v-6.44l-1.215-.139c-.093-.514.28-.886.747-.933zM2.081 1.333C3.153.4 4.459.026 6.09.166L18.38.926c1.495.14 1.869.42 2.802 1.12l3.921 2.799c.653.466.839.7.839 1.213v14.089c0 1.12-.373 1.773-1.588 1.866l-15.085.933c-.934.047-1.401-.14-1.915-.746L2.221 18.2c-.653-.793-.935-1.4-.935-2.146V2.5c0-.933.373-1.026.795-1.167z" />
  </svg>
);

const LinearLogo = () => (
  <svg viewBox="0 0 24 24" className="w-5 h-5">
    <path
      d="M3.152 14.192c-.166-.164-.166-.428 0-.592l7.048-6.956c.166-.164.434-.164.6 0l6.952 6.956c.166.164.166.428 0 .592l-7.048 6.956c-.166.164-.434.164-.6 0z"
      fill="#5E6AD2"
    />
  </svg>
);

const JiraLogo = () => (
  <svg viewBox="0 0 24 24" className="w-5 h-5">
    <path
      d="M12.005 0C8.41 0 5.705 3.105 5.705 6.523c0 .105 0 .21.007.315h-.007v10.639l6.3 6.523 6.295-6.523V6.838h-.007c.007-.105.007-.21.007-.315C18.3 3.105 15.596 0 12.005 0zm0 2.526c2.108 0 3.818 1.79 3.818 4 0 2.206-1.71 4-3.818 4s-3.818-1.794-3.818-4c0-2.21 1.71-4 3.818-4z"
      fill="#2684FF"
    />
  </svg>
);

// AI Client logos — official brand marks from HookSourceIcon
function ClaudeIcon({ className }: { className?: string }) {
  return (
    <svg
      className={className}
      viewBox="0 0 512 509.64"
      xmlns="http://www.w3.org/2000/svg"
    >
      <path
        fill="#D77655"
        d="M115.612 0h280.775C459.974 0 512 52.026 512 115.612v278.415c0 63.587-52.026 115.612-115.613 115.612H115.612C52.026 509.639 0 457.614 0 394.027V115.612C0 52.026 52.026 0 115.612 0z"
      />
      <path
        fill="#FCF2EE"
        fillRule="nonzero"
        d="M142.27 316.619l73.655-41.326 1.238-3.589-1.238-1.996-3.589-.001-12.31-.759-42.084-1.138-36.498-1.516-35.361-1.896-8.897-1.895-8.34-10.995.859-5.484 7.482-5.03 10.717.935 23.683 1.617 35.537 2.452 25.782 1.517 38.193 3.968h6.064l.86-2.451-2.073-1.517-1.618-1.517-36.776-24.922-39.81-26.338-20.852-15.166-11.273-7.683-5.687-7.204-2.451-15.721 10.237-11.273 13.75.935 3.513.936 13.928 10.716 29.749 23.027 38.848 28.612 5.687 4.727 2.275-1.617.278-1.138-2.553-4.271-21.13-38.193-22.546-38.848-10.035-16.101-2.654-9.655c-.935-3.968-1.617-7.304-1.617-11.374l11.652-15.823 6.445-2.073 15.545 2.073 6.547 5.687 9.655 22.092 15.646 34.78 24.265 47.291 7.103 14.028 3.791 12.992 1.416 3.968 2.449-.001v-2.275l1.997-26.641 3.69-32.707 3.589-42.084 1.239-11.854 5.863-14.206 11.652-7.683 9.099 4.348 7.482 10.716-1.036 6.926-4.449 28.915-8.72 45.294-5.687 30.331h3.313l3.792-3.791 15.342-20.372 25.782-32.227 11.374-12.789 13.27-14.129 8.517-6.724 16.1-.001 11.854 17.617-5.307 18.199-16.581 21.029-13.75 17.819-19.716 26.54-12.309 21.231 1.138 1.694 2.932-.278 44.536-9.479 24.062-4.347 28.714-4.928 12.992 6.066 1.416 6.167-5.106 12.613-30.71 7.583-36.018 7.204-53.636 12.689-.657.48.758.935 24.164 2.275 10.337.556h25.301l47.114 3.514 12.309 8.139 7.381 9.959-1.238 7.583-18.957 9.655-25.579-6.066-59.702-14.205-20.474-5.106-2.83-.001v1.694l17.061 16.682 31.266 28.233 39.152 36.397 1.997 8.999-5.03 7.102-5.307-.758-34.401-25.883-13.27-11.651-30.053-25.302-1.996-.001v2.654l6.926 10.136 36.574 54.975 1.895 16.859-2.653 5.485-9.479 3.311-10.414-1.895-21.408-30.054-22.092-33.844-17.819-30.331-2.173 1.238-10.515 113.261-4.929 5.788-11.374 4.348-9.478-7.204-5.03-11.652 5.03-23.027 6.066-30.052 4.928-23.886 4.449-29.674 2.654-9.858-.177-.657-2.173.278-22.37 30.71-34.021 45.977-26.919 28.815-6.445 2.553-11.173-5.789 1.037-10.337 6.243-9.2 37.257-47.392 22.47-29.371 14.508-16.961-.101-2.451h-.859l-98.954 64.251-17.618 2.275-7.583-7.103.936-11.652 3.589-3.791 29.749-20.474-.101.102.024.101z"
      />
    </svg>
  );
}

function CursorIcon({ className }: { className?: string }) {
  return (
    <svg
      className={className}
      viewBox="0 0 466.73 532.09"
      xmlns="http://www.w3.org/2000/svg"
    >
      <path
        fill="currentColor"
        d="M457.43,125.94L244.42,2.96c-6.84-3.95-15.28-3.95-22.12,0L9.3,125.94c-5.75,3.32-9.3,9.46-9.3,16.11v247.99c0,6.65,3.55,12.79,9.3,16.11l213.01,122.98c6.84,3.95,15.28,3.95,22.12,0l213.01-122.98c5.75-3.32,9.3-9.46,9.3-16.11v-247.99c0-6.65-3.55-12.79-9.3-16.11h-.01ZM444.05,151.99l-205.63,356.16c-1.39,2.4-5.06,1.42-5.06-1.36v-233.21c0-4.66-2.49-8.97-6.53-11.31L24.87,145.67c-2.4-1.39-1.42-5.06,1.36-5.06h411.26c5.84,0,9.49,6.33,6.57,11.39h-.01Z"
      />
    </svg>
  );
}

function CodexIcon({ className }: { className?: string }) {
  return (
    <svg
      className={className}
      viewBox="0 0 24 24"
      xmlns="http://www.w3.org/2000/svg"
    >
      <path
        fill="#10A37F"
        d="M22.282 9.821a5.985 5.985 0 00-.516-4.91 6.046 6.046 0 00-6.51-2.9A6.065 6.065 0 0012 .067a6.045 6.045 0 00-5.764 4.152 5.985 5.985 0 00-3.996 2.9 6.045 6.045 0 00.749 7.102 5.985 5.985 0 00.516 4.911 6.045 6.045 0 006.51 2.9A6.065 6.065 0 0012 23.933a6.045 6.045 0 005.764-4.152 5.985 5.985 0 003.996-2.9 6.045 6.045 0 00-.749-7.102"
      />
    </svg>
  );
}

function CopilotIcon({ className }: { className?: string }) {
  return (
    <svg
      className={className}
      viewBox="0 0 24 24"
      xmlns="http://www.w3.org/2000/svg"
    >
      <path
        fill="#0078D4"
        d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm0 3c1.66 0 3 1.34 3 3s-1.34 3-3 3-3-1.34-3-3 1.34-3 3-3zm0 14.2a7.2 7.2 0 01-6-3.22c.03-1.99 4-3.08 6-3.08 1.99 0 5.97 1.09 6 3.08a7.2 7.2 0 01-6 3.22z"
      />
    </svg>
  );
}

// Stacked card cluster — multiple overlapping cards to show many instances
function ClientCluster({
  icon: Icon,
  name,
  count,
  delay,
}: {
  icon: React.ComponentType<{ className?: string }>;
  name: string;
  count: number;
  delay: number;
}) {
  return (
    <motion.div
      initial={{ opacity: 0, scale: 0.9 }}
      animate={{ opacity: 1, scale: 1 }}
      transition={{ duration: 0.3, delay }}
      className="relative"
      style={{ paddingTop: (count - 1) * 5, paddingLeft: (count - 1) * 5 }}
    >
      {/* Shadow cards behind — stacked offset */}
      {Array.from({ length: count - 1 }).map((_, i) => (
        <div
          key={i}
          className="absolute bg-white border border-slate-200 rounded-lg shadow-sm"
          style={{
            top: i * 5,
            left: i * 5,
            right: (count - 1 - i) * 5,
            bottom: (count - 1 - i) * 5,
          }}
        />
      ))}
      {/* Front card */}
      <div className="relative flex items-center gap-2 px-3 py-2.5 bg-white border border-slate-200 rounded-lg shadow-sm">
        <Icon className="w-5 h-5" />
        <span className="text-xs font-medium text-slate-600">{name}</span>
      </div>
    </motion.div>
  );
}

// Mini chat app card — represents one deployed chat instance
function MiniChatApp({ label, delay }: { label: string; delay: number }) {
  return (
    <motion.div
      initial={{ opacity: 0, scale: 0.9 }}
      animate={{ opacity: 1, scale: 1 }}
      transition={{ duration: 0.25, delay }}
      className="bg-white border border-slate-200 rounded shadow-sm overflow-hidden"
    >
      <div className="flex items-center gap-1 px-2 py-1 bg-slate-100 border-b border-slate-200">
        <div className="flex gap-0.5">
          <div className="w-1.5 h-1.5 rounded-full bg-red-400" />
          <div className="w-1.5 h-1.5 rounded-full bg-yellow-400" />
          <div className="w-1.5 h-1.5 rounded-full bg-green-400" />
        </div>
        <span className="text-[7px] text-slate-400 font-mono ml-1">
          {label}
        </span>
      </div>
      <div className="p-1.5 flex gap-1">
        <div className="flex-1 flex flex-col gap-0.5">
          <div className="h-1 w-full bg-slate-100 rounded" />
          <div className="h-1 w-3/4 bg-slate-100 rounded" />
          <div className="h-1 w-1/2 bg-slate-100 rounded" />
        </div>
        <div className="w-8 bg-slate-50 rounded border border-slate-100 flex items-center justify-center">
          <svg
            className="w-2.5 h-2.5 text-slate-300"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="3"
          >
            <path d="M21 15a2 2 0 01-2 2H7l-4 4V5a2 2 0 012-2h14a2 2 0 012 2z" />
          </svg>
        </div>
      </div>
    </motion.div>
  );
}

// Distributed view — clustered AI clients and chat apps side by side
function DistributedClients({ delay }: { delay: number }) {
  return (
    <motion.div
      initial={{ opacity: 0, y: -10 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.4, delay }}
      className="flex w-full gap-4"
    >
      {/* Left: AI Client clusters */}
      <div className="flex-1">
        <div className="text-[10px] font-medium text-slate-400 uppercase tracking-wider mb-3">
          AI Agents
        </div>
        <div className="flex flex-wrap gap-4">
          <ClientCluster
            icon={ClaudeIcon}
            name="Claude"
            count={3}
            delay={delay + 0.1}
          />
          <ClientCluster
            icon={CursorIcon}
            name="Cursor"
            count={2}
            delay={delay + 0.2}
          />
          <ClientCluster
            icon={CodexIcon}
            name="Codex"
            count={2}
            delay={delay + 0.3}
          />
          <ClientCluster
            icon={CopilotIcon}
            name="Copilot"
            count={2}
            delay={delay + 0.4}
          />
        </div>
      </div>

      {/* Right: Chat apps */}
      <div className="flex-1">
        <div className="text-[10px] font-medium text-slate-400 uppercase tracking-wider mb-3">
          Product Agents
        </div>
        <div className="flex flex-col gap-1.5">
          <MiniChatApp label="support.co" delay={delay + 0.4} />
          <MiniChatApp label="sales-app" delay={delay + 0.45} />
          <MiniChatApp label="internal" delay={delay + 0.5} />
        </div>
      </div>
    </motion.div>
  );
}

// Narrow rectangle component for platform features
function FeatureBar({
  icon,
  label,
  delay,
}: {
  icon: React.ReactNode;
  label: string;
  delay: number;
}) {
  return (
    <motion.div
      initial={{ opacity: 0, x: -10 }}
      animate={{ opacity: 1, x: 0 }}
      transition={{ duration: 0.4, delay }}
      className="flex items-center gap-2 px-3 py-2 bg-slate-50 border border-slate-200 rounded text-xs text-slate-600"
    >
      <span className="text-slate-400">{icon}</span>
      <span className="font-medium">{label}</span>
    </motion.div>
  );
}

const PULSE_COLORS = [
  BRAND_COLORS.green, // #5A8250
  BRAND_COLORS.blue, // #2873D7
  BRAND_COLORS.orange, // #FB873F
];

function PulseConnector({
  delay = 0,
  disabled = false,
}: {
  delay?: number;
  disabled?: boolean;
}) {
  if (disabled) {
    return <div className="h-6 w-px bg-slate-300" />;
  }
  return (
    <div className="relative flex h-6 w-2 items-center justify-center overflow-hidden">
      {PULSE_COLORS.map((color, i) => (
        <motion.div
          key={color}
          className="absolute h-1 w-1 rounded-full"
          style={{ backgroundColor: color }}
          initial={{ y: 12, opacity: 0 }}
          animate={{ y: -12, opacity: [0, 1, 1, 0] }}
          transition={{
            duration: 2.5,
            delay: delay + i * 0.6,
            repeat: Infinity,
            ease: "easeInOut",
          }}
        />
      ))}
    </div>
  );
}

interface PlatformDiagramProps {
  className?: string;
}

export function PlatformDiagram({ className }: PlatformDiagramProps) {
  const prefersReducedMotion = useReducedMotion();
  return (
    <div
      className={cn(
        "relative w-full h-full flex flex-col justify-center",
        className,
      )}
    >
      <div className="flex flex-col items-center gap-4 max-w-md mx-auto py-6">
        {/* Top - Distributed AI clients and chat apps across the org */}
        <DistributedClients delay={0.1} />

        {/* Connection: Chat → Backend */}
        <PulseConnector delay={1.4} disabled={prefersReducedMotion ?? false} />

        {/* Chat Backend Section */}
        <motion.div
          initial={{ opacity: 0, y: 10 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.4, delay: 0.6 }}
          className="w-full bg-white border border-slate-200 rounded-lg p-3"
        >
          <div className="flex items-center mb-3">
            <GramLogo variant="horizontal" className="w-20" />
            <span className="text-[10px] font-medium text-slate-400 uppercase tracking-wider ml-1">
              Control Plane
            </span>
          </div>
          <div className="flex flex-col gap-1.5">
            <FeatureBar
              delay={0.7}
              icon={
                <svg
                  className="w-3.5 h-3.5"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                >
                  <path d="M12 20V10M18 20V4M6 20v-4" />
                </svg>
              }
              label="Usage insights"
            />
            <FeatureBar
              delay={0.75}
              icon={
                <svg
                  className="w-3.5 h-3.5"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                >
                  <path d="M10 13a5 5 0 007.54.54l3-3a5 5 0 00-7.07-7.07l-1.72 1.71" />
                  <path d="M14 11a5 5 0 00-7.54-.54l-3 3a5 5 0 007.07 7.07l1.71-1.71" />
                </svg>
              }
              label="Session hooks"
            />
            <FeatureBar
              delay={0.8}
              icon={
                <svg
                  className="w-3.5 h-3.5"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                >
                  <rect x="3" y="11" width="18" height="11" rx="2" />
                  <path d="M7 11V7a5 5 0 0110 0v4" />
                </svg>
              }
              label="Permissions & authorization"
            />
          </div>
        </motion.div>

        {/* Connection line */}
        <PulseConnector delay={1.1} disabled={prefersReducedMotion ?? false} />

        {/* Gram Platform - Center */}
        <motion.div
          initial={{ opacity: 0, scale: 0.95 }}
          animate={{ opacity: 1, scale: 1 }}
          transition={{ duration: 0.5, delay: 0.9 }}
          className="relative w-full"
        >
          {/* Gradient border */}
          <motion.div
            className="absolute -inset-[1.5px] rounded-lg"
            style={{
              background: `linear-gradient(135deg, ${BRAND_COLORS.green}, ${BRAND_COLORS.blue}, ${BRAND_COLORS.orange})`,
            }}
            animate={
              prefersReducedMotion ? undefined : { opacity: [0.7, 1, 0.7] }
            }
            transition={
              prefersReducedMotion
                ? undefined
                : { duration: 3, repeat: Infinity, ease: "easeInOut" }
            }
          />
          <div className="relative bg-white rounded-lg p-3">
            <div className="flex items-center mb-3">
              <GramLogo variant="horizontal" className="w-20" />
              <span className="text-[10px] font-medium text-slate-400 uppercase tracking-wider ml-1">
                Tools Platform
              </span>
            </div>
            <div className="flex flex-col gap-1.5">
              <FeatureBar
                delay={1.0}
                icon={
                  <svg
                    className="w-3.5 h-3.5"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="2"
                  >
                    <path d="M12 2L2 7v10l10 5 10-5V7L12 2z" />
                  </svg>
                }
                label="MCP server management"
              />
              <FeatureBar
                delay={1.05}
                icon={
                  <svg
                    className="w-3.5 h-3.5"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="2"
                  >
                    <path d="M4 19.5A2.5 2.5 0 016.5 17H20" />
                    <path d="M6.5 2H20v20H6.5A2.5 2.5 0 014 19.5v-15A2.5 2.5 0 016.5 2z" />
                  </svg>
                }
                label="Skills, plugins & CLIs"
              />
              <FeatureBar
                delay={1.1}
                icon={
                  <svg
                    className="w-3.5 h-3.5"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="2"
                  >
                    <path d="M12 19l7-7 3 3-7 7-3-3z" />
                    <path d="M18 13l-1.5-7.5L2 2l3.5 14.5L13 18l5-5z" />
                  </svg>
                }
                label="Tool curation & design"
              />
            </div>
          </div>
        </motion.div>

        {/* Connection line */}
        <PulseConnector delay={0.8} disabled={prefersReducedMotion ?? false} />

        {/* Bottom - Data Sources */}
        <div className="grid grid-cols-2 gap-3 w-full">
          {/* Your Data */}
          <motion.div
            initial={{ opacity: 0, y: 10 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.4, delay: 1.2 }}
            className="bg-white border border-slate-200 rounded-lg p-3"
          >
            <div className="text-[10px] font-medium text-slate-400 uppercase tracking-wider mb-2">
              Your Data
            </div>
            <div className="flex flex-col gap-1">
              <span className="text-xs text-slate-600 flex items-center gap-1.5">
                <svg
                  className="w-3 h-3 text-slate-400"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                >
                  <path d="M7 16V4m0 0L3 8m4-4l4 4M17 8v12m0 0l4-4m-4 4l-4-4" />
                </svg>
                APIs
              </span>
              <span className="text-xs text-slate-600 flex items-center gap-1.5">
                <svg
                  className="w-3 h-3 text-slate-400"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                >
                  <ellipse cx="12" cy="6" rx="8" ry="3" />
                  <path d="M4 6v6c0 1.66 3.58 3 8 3s8-1.34 8-3V6M4 12v6c0 1.66 3.58 3 8 3s8-1.34 8-3v-6" />
                </svg>
                Databases
              </span>
            </div>
          </motion.div>

          {/* Your SaaS */}
          <motion.div
            initial={{ opacity: 0, y: 10 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.4, delay: 1.3 }}
            className="bg-white border border-slate-200 rounded-lg p-3"
          >
            <div className="text-[10px] font-medium text-slate-400 uppercase tracking-wider mb-2">
              Your SaaS
            </div>
            <div className="grid grid-cols-3 gap-1.5">
              <div className="flex items-center justify-center p-1 bg-slate-50 rounded">
                <GitHubLogo />
              </div>
              <div className="flex items-center justify-center p-1 bg-slate-50 rounded">
                <FigmaLogo />
              </div>
              <div className="flex items-center justify-center p-1 bg-slate-50 rounded">
                <SlackLogo />
              </div>
              <div className="flex items-center justify-center p-1 bg-slate-50 rounded">
                <NotionLogo />
              </div>
              <div className="flex items-center justify-center p-1 bg-slate-50 rounded">
                <LinearLogo />
              </div>
              <div className="flex items-center justify-center p-1 bg-slate-50 rounded">
                <JiraLogo />
              </div>
            </div>
          </motion.div>
        </div>
      </div>
    </div>
  );
}
