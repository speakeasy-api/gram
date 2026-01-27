import { GramLogo } from "@/components/gram-logo";
import { cn } from "@/lib/utils";
import { motion } from "motion/react";

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

// AI Client logos
const CursorLogo = () => (
  <svg viewBox="0 0 24 24" className="w-4 h-4" fill="currentColor">
    <path d="M5 3l14 9-14 9V3z" />
  </svg>
);

const ClaudeCodeLogo = () => (
  <svg viewBox="0 0 24 24" className="w-4 h-4">
    <circle cx="12" cy="12" r="10" fill="#D97706" />
    <path
      d="M8 12h8M12 8v8"
      stroke="#fff"
      strokeWidth="2"
      strokeLinecap="round"
    />
  </svg>
);

const WindsurfLogo = () => (
  <svg viewBox="0 0 24 24" className="w-4 h-4" fill="currentColor">
    <path d="M12 2L2 22h20L12 2zm0 6l6 12H6l6-12z" />
  </svg>
);

// Agent logos
const OpenAILogo = () => (
  <svg viewBox="0 0 24 24" className="w-4 h-4" fill="currentColor">
    <path d="M22.282 9.821a5.985 5.985 0 00-.516-4.91 6.046 6.046 0 00-6.51-2.9A6.065 6.065 0 0012 .067a6.045 6.045 0 00-5.764 4.152 5.985 5.985 0 00-3.996 2.9 6.045 6.045 0 00.749 7.102 5.985 5.985 0 00.516 4.911 6.045 6.045 0 006.51 2.9A6.065 6.065 0 0012 23.933a6.045 6.045 0 005.764-4.152 5.985 5.985 0 003.996-2.9 6.045 6.045 0 00-.749-7.102" />
  </svg>
);

const LangChainLogo = () => (
  <svg viewBox="0 0 24 24" className="w-4 h-4">
    <path d="M12 2a10 10 0 100 20 10 10 0 000-20z" fill="#1C3C3C" />
    <path
      d="M8 12h8M12 8v8"
      stroke="#fff"
      strokeWidth="2"
      strokeLinecap="round"
    />
  </svg>
);

// Product mockup with embedded chat
function ProductWithChat({ delay }: { delay: number }) {
  return (
    <motion.div
      initial={{ opacity: 0, y: -20 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.5, delay }}
      className="w-full bg-white border border-slate-200 rounded-lg shadow-sm overflow-hidden"
    >
      {/* Browser chrome */}
      <div className="flex items-center gap-1.5 px-3 py-2 bg-slate-100 border-b border-slate-200">
        <div className="flex gap-1.5">
          <div className="w-2.5 h-2.5 rounded-full bg-red-400" />
          <div className="w-2.5 h-2.5 rounded-full bg-yellow-400" />
          <div className="w-2.5 h-2.5 rounded-full bg-green-400" />
        </div>
        <div className="flex-1 mx-4">
          <div className="bg-white rounded px-3 py-1 text-[10px] text-slate-400 font-mono">
            your-app.com
          </div>
        </div>
      </div>

      {/* App content */}
      <div className="flex h-32">
        {/* Main content area */}
        <div className="flex-1 p-3 border-r border-slate-100">
          <div className="h-2 w-20 bg-slate-200 rounded mb-2" />
          <div className="h-2 w-32 bg-slate-100 rounded mb-1.5" />
          <div className="h-2 w-28 bg-slate-100 rounded mb-1.5" />
          <div className="h-2 w-24 bg-slate-100 rounded mb-3" />
          <div className="flex gap-2">
            <div className="h-6 w-16 bg-slate-100 rounded" />
            <div className="h-6 w-16 bg-slate-100 rounded" />
          </div>
        </div>

        {/* Embedded chat widget */}
        <div className="w-36 bg-slate-50 flex flex-col">
          <div className="px-2 py-1.5 border-b border-slate-200 flex items-center gap-1.5">
            <div className="w-4 h-4 rounded bg-gradient-to-br from-blue-500 to-emerald-500 flex items-center justify-center">
              <svg
                className="w-2.5 h-2.5 text-white"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="3"
              >
                <path d="M21 15a2 2 0 01-2 2H7l-4 4V5a2 2 0 012-2h14a2 2 0 012 2z" />
              </svg>
            </div>
            <span className="text-[9px] font-medium text-slate-600">
              AI Assistant
            </span>
          </div>
          <div className="flex-1 p-2 flex flex-col gap-1.5 overflow-hidden">
            <div className="bg-slate-200 rounded-lg px-2 py-1 text-[8px] text-slate-600 self-start max-w-[90%]">
              How can I help?
            </div>
            <div className="bg-blue-500 rounded-lg px-2 py-1 text-[8px] text-white self-end max-w-[90%]">
              Create a report
            </div>
            <motion.div
              className="bg-slate-200 rounded-lg px-2 py-1 text-[8px] text-slate-600 self-start"
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              transition={{ delay: delay + 0.8, duration: 0.3 }}
            >
              <motion.span
                initial={{ opacity: 0 }}
                animate={{ opacity: [0, 1, 0] }}
                transition={{
                  delay: delay + 0.8,
                  duration: 1.2,
                  repeat: Infinity,
                }}
              >
                ●●●
              </motion.span>
            </motion.div>
          </div>
          <div className="px-2 pb-2">
            <div className="bg-white border border-slate-200 rounded px-2 py-1 text-[8px] text-slate-400">
              Type a message...
            </div>
          </div>
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

interface PlatformDiagramProps {
  className?: string;
}

export function PlatformDiagram({ className }: PlatformDiagramProps) {
  return (
    <div
      className={cn(
        "relative w-full h-full flex flex-col justify-center",
        className,
      )}
    >
      <div className="flex flex-col items-center gap-4 max-w-md mx-auto py-6">
        {/* Top - Product with embedded chat */}
        <div className="w-full">
          <ProductWithChat delay={0.2} />
        </div>

        {/* Connection line */}
        <motion.div
          initial={{ scaleY: 0 }}
          animate={{ scaleY: 1 }}
          transition={{ duration: 0.3, delay: 0.5 }}
          className="w-px h-6 bg-slate-300 origin-top"
        />

        {/* Chat Backend Section */}
        <motion.div
          initial={{ opacity: 0, y: 10 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.4, delay: 0.6 }}
          className="w-full bg-white border border-slate-200 rounded-lg p-3"
        >
          <div className="flex items-center mb-3">
            <GramLogo variant="horizontal" className="w-16" />
            <span className="text-[10px] font-medium text-slate-400 uppercase tracking-wider ml-2">
              Chat Backend
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
                  <path d="M21 15a2 2 0 01-2 2H7l-4 4V5a2 2 0 012-2h14a2 2 0 012 2z" />
                </svg>
              }
              label="Chat logs and resolution"
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
                  <circle cx="12" cy="12" r="3" />
                  <path d="M12 1v4M12 19v4M4.22 4.22l2.83 2.83M16.95 16.95l2.83 2.83M1 12h4M19 12h4" />
                </svg>
              }
              label="Agent orchestration"
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
                  <path d="M17 21v-2a4 4 0 00-4-4H5a4 4 0 00-4 4v2" />
                  <circle cx="9" cy="7" r="4" />
                  <path d="M23 21v-2a4 4 0 00-3-3.87M16 3.13a4 4 0 010 7.75" />
                </svg>
              }
              label="Session management"
            />
          </div>
        </motion.div>

        {/* Connection line */}
        <motion.div
          initial={{ scaleY: 0 }}
          animate={{ scaleY: 1 }}
          transition={{ duration: 0.3, delay: 0.85 }}
          className="w-px h-6 bg-slate-300 origin-top"
        />

        {/* Gram Platform - Center */}
        <motion.div
          initial={{ opacity: 0, scale: 0.95 }}
          animate={{ opacity: 1, scale: 1 }}
          transition={{ duration: 0.5, delay: 0.9 }}
          className="relative w-full"
        >
          {/* Gradient border */}
          <div
            className="absolute -inset-[1.5px] rounded-lg"
            style={{
              background: `linear-gradient(135deg, ${BRAND_COLORS.green}, ${BRAND_COLORS.blue}, ${BRAND_COLORS.orange})`,
            }}
          />
          <div className="relative bg-white rounded-lg p-3">
            <div className="flex items-center mb-3">
              <GramLogo variant="horizontal" className="w-16" />
              <span className="text-[10px] font-medium text-slate-400 uppercase tracking-wider ml-2">
                Tool Management
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
                    <rect x="3" y="11" width="18" height="11" rx="2" />
                    <path d="M7 11V7a5 5 0 0110 0v4" />
                  </svg>
                }
                label="Authentication & authorization"
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
        <motion.div
          initial={{ scaleY: 0 }}
          animate={{ scaleY: 1 }}
          transition={{ duration: 0.3, delay: 1.15 }}
          className="w-px h-6 bg-slate-300 origin-top"
        />

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
