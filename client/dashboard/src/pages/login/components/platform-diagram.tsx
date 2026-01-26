import { motion } from "motion/react";
import { cn } from "@/lib/utils";

// Brand gradient colors
const BRAND_COLORS = {
  green: "#5A8250",
  blue: "#2873D7",
  orange: "#FB873F",
  lightBlue: "#9BC3FF",
  slate: "#64748b",
};

// Service logo components (simplified SVG icons)
const GitHubLogo = () => (
  <svg viewBox="0 0 24 24" className="w-6 h-6" fill="currentColor">
    <path d="M12 0C5.37 0 0 5.37 0 12c0 5.31 3.435 9.795 8.205 11.385.6.105.825-.255.825-.57 0-.285-.015-1.23-.015-2.235-3.015.555-3.795-.735-4.035-1.41-.135-.345-.72-1.41-1.23-1.695-.42-.225-1.02-.78-.015-.795.945-.015 1.62.87 1.845 1.23 1.08 1.815 2.805 1.305 3.495.99.105-.78.42-1.305.765-1.605-2.67-.3-5.46-1.335-5.46-5.925 0-1.305.465-2.385 1.23-3.225-.12-.3-.54-1.53.12-3.18 0 0 1.005-.315 3.3 1.23.96-.27 1.98-.405 3-.405s2.04.135 3 .405c2.295-1.56 3.3-1.23 3.3-1.23.66 1.65.24 2.88.12 3.18.765.84 1.23 1.905 1.23 3.225 0 4.605-2.805 5.625-5.475 5.925.435.375.81 1.095.81 2.22 0 1.605-.015 2.895-.015 3.3 0 .315.225.69.825.57A12.02 12.02 0 0024 12c0-6.63-5.37-12-12-12z" />
  </svg>
);

const FigmaLogo = () => (
  <svg viewBox="0 0 24 24" className="w-6 h-6">
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

const MondayLogo = () => (
  <svg viewBox="0 0 24 24" className="w-6 h-6">
    <circle cx="4" cy="17" r="3" fill="#FF3D57" />
    <circle cx="12" cy="12" r="3" fill="#FFCB00" />
    <circle cx="20" cy="7" r="3" fill="#00D647" />
    <path
      d="M4 14v-4a3 3 0 016 0v4"
      fill="none"
      stroke="#FF3D57"
      strokeWidth="2"
    />
    <path
      d="M12 9v-2a3 3 0 016 0v2"
      fill="none"
      stroke="#FFCB00"
      strokeWidth="2"
    />
  </svg>
);

const AirtableLogo = () => (
  <svg viewBox="0 0 24 24" className="w-6 h-6">
    <path d="M11.5 2.5L2 7v10l9.5 5 9.5-5V7l-9.5-4.5z" fill="#FCB400" />
    <path d="M11.5 2.5L2 7l9.5 4.5L21 7l-9.5-4.5z" fill="#18BFFF" />
    <path d="M11.5 11.5V22l9.5-5V7l-9.5 4.5z" fill="#F82B60" />
  </svg>
);

const GmailLogo = () => (
  <svg viewBox="0 0 24 24" className="w-6 h-6">
    <path
      d="M2 6l10 7 10-7v12H2V6z"
      fill="#fff"
      stroke="#EA4335"
      strokeWidth="1.5"
    />
    <path d="M2 6l10 7 10-7" fill="none" stroke="#EA4335" strokeWidth="1.5" />
  </svg>
);

const PostmanLogo = () => (
  <svg viewBox="0 0 24 24" className="w-6 h-6">
    <circle cx="12" cy="12" r="10" fill="#FF6C37" />
    <path
      d="M8 12l3 3 5-6"
      fill="none"
      stroke="#fff"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
    />
  </svg>
);

const ConstantContactLogo = () => (
  <svg viewBox="0 0 24 24" className="w-6 h-6">
    <circle cx="12" cy="12" r="10" fill="#0076BE" />
    <path
      d="M8 12a4 4 0 108 0 4 4 0 00-8 0z"
      fill="none"
      stroke="#fff"
      strokeWidth="1.5"
    />
  </svg>
);

const AsanaLogo = () => (
  <svg viewBox="0 0 24 24" className="w-6 h-6">
    <circle cx="12" cy="6" r="4" fill="#F06A6A" />
    <circle cx="6" cy="16" r="4" fill="#F06A6A" />
    <circle cx="18" cy="16" r="4" fill="#F06A6A" />
  </svg>
);

// AI Client logos
const CursorLogo = () => (
  <svg viewBox="0 0 24 24" className="w-5 h-5" fill="currentColor">
    <path d="M5 3l14 9-14 9V3z" />
  </svg>
);

const ClaudeCodeLogo = () => (
  <svg viewBox="0 0 24 24" className="w-5 h-5">
    <circle cx="12" cy="12" r="10" fill="#D97706" />
    <path
      d="M8 12h8M12 8v8"
      stroke="#fff"
      strokeWidth="2"
      strokeLinecap="round"
    />
  </svg>
);

const CodexLogo = () => (
  <svg viewBox="0 0 24 24" className="w-5 h-5">
    <rect x="4" y="4" width="16" height="16" rx="2" fill="#10A37F" />
    <path
      d="M8 12l2 2 4-4"
      stroke="#fff"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
    />
  </svg>
);

const CopilotLogo = () => (
  <svg viewBox="0 0 24 24" className="w-5 h-5" fill="currentColor">
    <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm-2 15l-5-5 1.41-1.41L10 14.17l7.59-7.59L19 8l-9 9z" />
  </svg>
);

// Agent logos
const OpenAILogo = () => (
  <svg viewBox="0 0 24 24" className="w-5 h-5" fill="currentColor">
    <path d="M22.282 9.821a5.985 5.985 0 00-.516-4.91 6.046 6.046 0 00-6.51-2.9A6.065 6.065 0 0012 .067a6.045 6.045 0 00-5.764 4.152 5.985 5.985 0 00-3.996 2.9 6.045 6.045 0 00.749 7.102 5.985 5.985 0 00.516 4.911 6.045 6.045 0 006.51 2.9A6.065 6.065 0 0012 23.933a6.045 6.045 0 005.764-4.152 5.985 5.985 0 003.996-2.9 6.045 6.045 0 00-.749-7.102" />
  </svg>
);

const MastraLogo = () => (
  <svg viewBox="0 0 24 24" className="w-5 h-5">
    <path
      d="M12 2L2 7l10 5 10-5-10-5zM2 17l10 5 10-5M2 12l10 5 10-5"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
    />
  </svg>
);

const LangChainLogo = () => (
  <svg viewBox="0 0 24 24" className="w-5 h-5">
    <path d="M12 2a10 10 0 100 20 10 10 0 000-20z" fill="#1C3C3C" />
    <path
      d="M8 12h8M12 8v8"
      stroke="#fff"
      strokeWidth="2"
      strokeLinecap="round"
    />
  </svg>
);

const N8nLogo = () => (
  <svg viewBox="0 0 24 24" className="w-5 h-5">
    <rect x="2" y="2" width="20" height="20" rx="4" fill="#EA4B71" />
    <text
      x="12"
      y="16"
      textAnchor="middle"
      fill="#fff"
      fontSize="10"
      fontWeight="bold"
    >
      n8n
    </text>
  </svg>
);

// Gram logo for center
const GramLogoIcon = () => (
  <svg
    viewBox="0 0 24 24"
    className="w-6 h-6"
    fill="none"
    stroke="currentColor"
    strokeWidth="1.5"
  >
    <path d="M12 2L2 7v10l10 5 10-5V7L12 2z" />
    <path d="M12 7l-5 2.5v5L12 17l5-2.5v-5L12 7z" />
  </svg>
);

// Card component for the diagram sections
interface CardProps {
  children: React.ReactNode;
  className?: string;
  delay?: number;
  direction?: "left" | "right" | "up" | "down";
  style?: React.CSSProperties;
}

function Card({
  children,
  className,
  delay = 0,
  direction = "left",
  style,
}: CardProps) {
  const directions = {
    left: { x: -30, y: 0 },
    right: { x: 30, y: 0 },
    up: { x: 0, y: -20 },
    down: { x: 0, y: 20 },
  };

  return (
    <motion.div
      initial={{ opacity: 0, ...directions[direction] }}
      animate={{ opacity: 1, x: 0, y: 0 }}
      transition={{ duration: 0.6, delay, ease: "easeOut" }}
      className={cn(
        "bg-white rounded-xl border border-slate-200 shadow-sm p-4",
        className,
      )}
      style={style}
    >
      {children}
    </motion.div>
  );
}

// Animated connection line
interface ConnectionLineProps {
  className?: string;
  delay?: number;
  color?: string;
  direction?: "horizontal" | "vertical";
}

function ConnectionLine({
  className,
  delay = 0,
  color = BRAND_COLORS.slate,
  direction = "horizontal",
}: ConnectionLineProps) {
  return (
    <motion.div
      initial={{ opacity: 0, scale: 0 }}
      animate={{ opacity: 1, scale: 1 }}
      transition={{ duration: 0.4, delay }}
      className={cn("relative", className)}
    >
      <svg
        className={cn(
          "w-full h-full",
          direction === "vertical" ? "rotate-90" : "",
        )}
        viewBox="0 0 60 20"
        fill="none"
        preserveAspectRatio="none"
      >
        <motion.path
          d="M0 10 L50 10"
          stroke={color}
          strokeWidth="2"
          strokeDasharray="4 4"
          initial={{ pathLength: 0 }}
          animate={{ pathLength: 1 }}
          transition={{ duration: 0.8, delay: delay + 0.2 }}
        />
        <motion.path
          d="M45 5 L55 10 L45 15"
          stroke={color}
          strokeWidth="2"
          fill="none"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          transition={{ duration: 0.3, delay: delay + 0.8 }}
        />
      </svg>
      {/* Animated dot flowing along the line */}
      <motion.div
        className="absolute top-1/2 -translate-y-1/2 w-2 h-2 rounded-full"
        style={{ backgroundColor: color }}
        initial={{ left: "0%", opacity: 0 }}
        animate={{ left: ["0%", "85%"], opacity: [0, 1, 1, 0] }}
        transition={{
          duration: 2,
          delay: delay + 1,
          repeat: Infinity,
          repeatDelay: 1,
          ease: "easeInOut",
        }}
      />
    </motion.div>
  );
}

interface PlatformDiagramProps {
  className?: string;
}

export function PlatformDiagram({ className }: PlatformDiagramProps) {
  return (
    <div className={cn("relative w-full", className)}>
      {/* CSS for flowing animation */}
      <style>{`
        @keyframes flowDash {
          0% { stroke-dashoffset: 24; }
          100% { stroke-dashoffset: 0; }
        }
        .flow-line {
          animation: flowDash 1.5s linear infinite;
        }
        @keyframes float {
          0%, 100% { transform: translateY(0); }
          50% { transform: translateY(-4px); }
        }
        .float-card {
          animation: float 4s ease-in-out infinite;
        }
      `}</style>

      <div className="grid grid-cols-[1fr_auto_1fr] gap-4 items-center max-w-4xl mx-auto">
        {/* Left Column - Inputs */}
        <div className="space-y-4">
          {/* Your Data Card */}
          <Card delay={0.3} direction="left" className="float-card">
            <div className="text-xs font-semibold text-slate-500 uppercase tracking-wider mb-3">
              Your Data
            </div>
            <div className="flex flex-wrap gap-2">
              <span className="inline-flex items-center gap-1.5 px-2.5 py-1 bg-slate-100 rounded-md text-xs text-slate-600">
                <svg
                  className="w-3 h-3"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                >
                  <path d="M7 16V4m0 0L3 8m4-4l4 4M17 8v12m0 0l4-4m-4 4l-4-4" />
                </svg>
                APIs
              </span>
              <span className="inline-flex items-center gap-1.5 px-2.5 py-1 bg-slate-100 rounded-md text-xs text-slate-600">
                <svg
                  className="w-3 h-3"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                >
                  <ellipse cx="12" cy="6" rx="8" ry="3" />
                  <path d="M4 6v6c0 1.66 3.58 3 8 3s8-1.34 8-3V6" />
                  <path d="M4 12v6c0 1.66 3.58 3 8 3s8-1.34 8-3v-6" />
                </svg>
                Databases
              </span>
              <span className="inline-flex items-center gap-1.5 px-2.5 py-1 bg-slate-100 rounded-md text-xs text-slate-600">
                <svg
                  className="w-3 h-3"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                >
                  <rect x="3" y="3" width="18" height="18" rx="2" />
                  <path d="M3 9h18M9 21V9" />
                </svg>
                Data warehouses
              </span>
            </div>
          </Card>

          {/* Your SaaS Card */}
          <Card
            delay={0.5}
            direction="left"
            className="float-card"
            style={{ animationDelay: "0.5s" }}
          >
            <div className="text-xs font-semibold text-slate-500 uppercase tracking-wider mb-3">
              Your SaaS
            </div>
            <div className="grid grid-cols-4 gap-3">
              <div className="flex items-center justify-center p-2 bg-slate-50 rounded-lg hover:bg-slate-100 transition-colors">
                <GitHubLogo />
              </div>
              <div className="flex items-center justify-center p-2 bg-slate-50 rounded-lg hover:bg-slate-100 transition-colors">
                <FigmaLogo />
              </div>
              <div className="flex items-center justify-center p-2 bg-slate-50 rounded-lg hover:bg-slate-100 transition-colors">
                <MondayLogo />
              </div>
              <div className="flex items-center justify-center p-2 bg-slate-50 rounded-lg hover:bg-slate-100 transition-colors">
                <AirtableLogo />
              </div>
              <div className="flex items-center justify-center p-2 bg-slate-50 rounded-lg hover:bg-slate-100 transition-colors">
                <GmailLogo />
              </div>
              <div className="flex items-center justify-center p-2 bg-slate-50 rounded-lg hover:bg-slate-100 transition-colors">
                <PostmanLogo />
              </div>
              <div className="flex items-center justify-center p-2 bg-slate-50 rounded-lg hover:bg-slate-100 transition-colors">
                <ConstantContactLogo />
              </div>
              <div className="flex items-center justify-center p-2 bg-slate-50 rounded-lg hover:bg-slate-100 transition-colors">
                <AsanaLogo />
              </div>
            </div>
          </Card>
        </div>

        {/* Center Column - Gram Platform + Connections */}
        <div className="flex flex-col items-center gap-4 relative py-8">
          {/* Top outputs */}
          <motion.div
            initial={{ opacity: 0, y: -20 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.5, delay: 0.6 }}
            className="flex gap-2"
          >
            <div className="flex items-center gap-1.5 px-3 py-1.5 bg-white border border-slate-200 rounded-lg text-xs font-medium text-slate-600 shadow-sm">
              <svg
                className="w-3.5 h-3.5"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
              >
                <path d="M21 15a2 2 0 01-2 2H7l-4 4V5a2 2 0 012-2h14a2 2 0 012 2z" />
              </svg>
              Chat
            </div>
            <div className="flex items-center gap-1.5 px-3 py-1.5 bg-white border border-slate-200 rounded-lg text-xs font-medium text-slate-600 shadow-sm">
              <svg
                className="w-3.5 h-3.5"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
              >
                <path d="M12 2L2 7v10l10 5 10-5V7L12 2z" />
              </svg>
              MCP
            </div>
            <div className="flex items-center gap-1.5 px-3 py-1.5 bg-white border border-slate-200 rounded-lg text-xs font-medium text-slate-600 shadow-sm">
              <svg
                className="w-3.5 h-3.5"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
              >
                <circle cx="12" cy="12" r="3" />
                <path d="M12 1v4M12 19v4M4.22 4.22l2.83 2.83M16.95 16.95l2.83 2.83M1 12h4M19 12h4M4.22 19.78l2.83-2.83M16.95 7.05l2.83-2.83" />
              </svg>
              Agents
            </div>
          </motion.div>

          {/* Connection lines to top */}
          <motion.div
            initial={{ opacity: 0, scaleY: 0 }}
            animate={{ opacity: 1, scaleY: 1 }}
            transition={{ duration: 0.4, delay: 0.8 }}
            className="h-6 w-px bg-gradient-to-t from-slate-300 to-transparent origin-bottom"
          />

          {/* Gram Platform - Center Card with gradient border */}
          <motion.div
            initial={{ opacity: 0, scale: 0.9 }}
            animate={{ opacity: 1, scale: 1 }}
            transition={{ duration: 0.5, delay: 0 }}
            className="relative"
          >
            {/* Gradient border wrapper */}
            <div
              className="absolute -inset-[2px] rounded-2xl"
              style={{
                background: `linear-gradient(135deg, ${BRAND_COLORS.green}, ${BRAND_COLORS.blue}, ${BRAND_COLORS.orange})`,
              }}
            />
            <div className="relative bg-white rounded-2xl p-5 min-w-[200px]">
              <div className="flex items-center gap-2 mb-4">
                <GramLogoIcon />
                <span className="text-sm font-semibold text-slate-800">
                  gram
                </span>
                <span className="text-xs text-slate-400 uppercase tracking-wider">
                  Platform
                </span>
              </div>
              <div className="space-y-2">
                <div className="flex items-center gap-2 px-3 py-2 bg-slate-50 rounded-lg text-xs text-slate-600">
                  <svg
                    className="w-3.5 h-3.5 text-slate-400"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="2"
                  >
                    <path d="M12 2L2 7v10l10 5 10-5V7L12 2z" />
                  </svg>
                  MCP management
                </div>
                <div className="flex items-center gap-2 px-3 py-2 bg-slate-50 rounded-lg text-xs text-slate-600">
                  <svg
                    className="w-3.5 h-3.5 text-slate-400"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="2"
                  >
                    <rect x="3" y="11" width="18" height="11" rx="2" />
                    <path d="M7 11V7a5 5 0 0110 0v4" />
                  </svg>
                  Authentication
                </div>
                <div className="flex items-center gap-2 px-3 py-2 bg-slate-50 rounded-lg text-xs text-slate-600">
                  <svg
                    className="w-3.5 h-3.5 text-slate-400"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="2"
                  >
                    <path d="M12 19l7-7 3 3-7 7-3-3z" />
                    <path d="M18 13l-1.5-7.5L2 2l3.5 14.5L13 18l5-5z" />
                  </svg>
                  Tool design
                </div>
              </div>
            </div>
          </motion.div>

          {/* Left/Right connection lines */}
          <div className="absolute left-0 top-1/2 -translate-x-full -translate-y-1/2 w-8">
            <ConnectionLine delay={0.9} color={BRAND_COLORS.green} />
          </div>
          <div className="absolute right-0 top-1/2 translate-x-full -translate-y-1/2 w-8 rotate-180">
            <ConnectionLine delay={1.0} color={BRAND_COLORS.blue} />
          </div>

          {/* Bottom connection line with return arrow */}
          <motion.div
            initial={{ opacity: 0, scaleY: 0 }}
            animate={{ opacity: 1, scaleY: 1 }}
            transition={{ duration: 0.4, delay: 0.9 }}
            className="h-6 w-px bg-gradient-to-b from-slate-300 to-transparent origin-top"
          />
        </div>

        {/* Right Column - Outputs */}
        <div className="space-y-4">
          {/* Your AI Clients Card */}
          <Card
            delay={0.7}
            direction="right"
            className="float-card"
            style={{ animationDelay: "1s" }}
          >
            <div className="text-xs font-semibold text-slate-500 uppercase tracking-wider mb-3">
              Your AI Clients
            </div>
            <div className="grid grid-cols-2 gap-2">
              <div className="flex items-center gap-2 px-2 py-1.5 bg-slate-50 rounded-md text-xs text-slate-700">
                <CursorLogo />
                <span>Cursor</span>
              </div>
              <div className="flex items-center gap-2 px-2 py-1.5 bg-slate-50 rounded-md text-xs text-slate-700">
                <ClaudeCodeLogo />
                <span>Claude Code</span>
              </div>
              <div className="flex items-center gap-2 px-2 py-1.5 bg-slate-50 rounded-md text-xs text-slate-700">
                <CodexLogo />
                <span>Codex</span>
              </div>
              <div className="flex items-center gap-2 px-2 py-1.5 bg-slate-50 rounded-md text-xs text-slate-700">
                <CopilotLogo />
                <span>GitHub Copilot</span>
              </div>
            </div>
          </Card>

          {/* Your Agents Card */}
          <Card
            delay={0.9}
            direction="right"
            className="float-card"
            style={{ animationDelay: "1.5s" }}
          >
            <div className="text-xs font-semibold text-slate-500 uppercase tracking-wider mb-3">
              Your Agents
            </div>
            <div className="grid grid-cols-2 gap-2">
              <div className="flex items-center gap-2 px-2 py-1.5 bg-slate-50 rounded-md text-xs text-slate-700">
                <OpenAILogo />
                <span>OpenAI</span>
              </div>
              <div className="flex items-center gap-2 px-2 py-1.5 bg-slate-50 rounded-md text-xs text-slate-700">
                <MastraLogo />
                <span>mastra</span>
              </div>
              <div className="flex items-center gap-2 px-2 py-1.5 bg-slate-50 rounded-md text-xs text-slate-700">
                <LangChainLogo />
                <span>LangChain</span>
              </div>
              <div className="flex items-center gap-2 px-2 py-1.5 bg-slate-50 rounded-md text-xs text-slate-700">
                <N8nLogo />
                <span>n8n</span>
              </div>
            </div>
          </Card>
        </div>
      </div>
    </div>
  );
}
