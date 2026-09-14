package aivendors

// registry is Gram's curated list of AI products.
//
// Every Document here becomes a CIMD admission entry. A MISSING one is a hard,
// unrecoverable auth failure: MCP clients pick CIMD over dynamic registration
// once at discovery and do not fall back, so the user sees an OAuth error with
// no recourse. Be generous — an extra document costs a string comparison.
//
// Verify every document live before it lands (HTTP 200, valid JSON, client_id
// equal to URL, token_endpoint_auth_method "none") and record the date above
// the product. Every product with Signatures is also a scan target.
//
// Declaration order is load-bearing: wildcards match in order, so reordering
// can change which entry a client_id is attributed to.
var registry = []Product{
	// Verified 2026-07. Claude Code selects CIMD when the AS advertises
	// client_id_metadata_document_supported AND "none" among its
	// token_endpoint_auth_methods_supported.
	//
	// Claude Code and Claude are separate products under one vendor key, so
	// neither may be named by `anthropic` — blocking the CLI must not take
	// the chat app with it. GatewayMatchersFor derives that.
	{
		ID:          "claude-code",
		VendorKey:   "anthropic",
		DisplayName: "Claude Code",
		Category:    CategoryHarness,
		Signatures: Signatures{
			BundleIDs:    nil,
			Binaries:     []string{"claude"},
			ConfigDirs:   []string{"~/.claude"},
			ProcessNames: []string{"claude"},
		},
		VersionPlistKey: "",
		ClientInfoNames: []string{"claude-code", "Claude Code", "claude-ai"},
		Documents: []Document{{
			URL:         "https://claude.ai/oauth/claude-code-client-metadata",
			DisplayName: "Anthropic (Claude Code)",
			DisplayOnly: false,
			Enabled:     true,
		}},
	},
	{
		ID:              "claude",
		VendorKey:       "anthropic",
		DisplayName:     "Claude",
		Category:        "",
		Signatures:      Signatures{BundleIDs: nil, Binaries: nil, ConfigDirs: nil, ProcessNames: nil},
		VersionPlistKey: "",
		ClientInfoNames: nil,
		Documents: []Document{{
			URL:         "https://claude.ai/oauth/mcp-oauth-client-metadata",
			DisplayName: "Anthropic (Claude)",
			DisplayOnly: false,
			Enabled:     true,
		}},
	},

	// Verified 2026-07. Insiders ships a distinct document; both are literal
	// constants in the VS Code source. One product, two documents — which is
	// what the vendor key is for, and why `microsoft` names it.
	{
		ID:              "vscode",
		VendorKey:       "microsoft",
		DisplayName:     "Visual Studio Code",
		Category:        "",
		Signatures:      Signatures{BundleIDs: nil, Binaries: nil, ConfigDirs: nil, ProcessNames: nil},
		VersionPlistKey: "",
		ClientInfoNames: nil,
		Documents: []Document{
			{
				URL:         "https://vscode.dev/oauth/client-metadata.json",
				DisplayName: "Visual Studio Code",
				DisplayOnly: false,
				Enabled:     true,
			},
			{
				URL:         "https://insiders.vscode.dev/oauth/client-metadata.json",
				DisplayName: "Visual Studio Code (Insiders)",
				DisplayOnly: false,
				Enabled:     true,
			},
		},
	},

	// Verified 2026-07. Signatures verified 2026-09-11 against Zed's
	// configuring-zed docs: settings live at ~/.config/zed/settings.json on
	// both macOS and Linux. The macOS app also keeps state under
	// ~/Library/Application Support/Zed, which the config dir above already
	// covers for detection purposes.
	{
		ID:          "zed",
		VendorKey:   "zed",
		DisplayName: "Zed",
		Category:    CategoryHarness,
		Signatures: Signatures{
			BundleIDs:    []string{"dev.zed.Zed"},
			Binaries:     nil,
			ConfigDirs:   []string{"~/.config/zed"},
			ProcessNames: []string{"Zed"},
		},
		VersionPlistKey: "",
		ClientInfoNames: nil,
		Documents: []Document{{
			URL:         "https://zed.dev/oauth/client-metadata.json",
			DisplayName: "Zed",
			DisplayOnly: false,
			Enabled:     true,
		}},
	},

	// Verified 2026-07. CIMD shipped in goose v1.32.0 (2026-04-23).
	//
	// Signatures verified 2026-09-11: the CLI and the desktop app share
	// ~/.config/goose/config.yaml, and the CLI is invoked as `goose`.
	//
	// Filed as a harness because that is what it is used for and how every
	// roundup lists it, but Block describes it as a general-purpose agent
	// that "goes beyond code suggestions" — it is the entry in this registry
	// most arguable as an assistant.
	{
		ID:          "goose",
		VendorKey:   "block",
		DisplayName: "Goose",
		Category:    CategoryHarness,
		Signatures: Signatures{
			BundleIDs:    nil,
			Binaries:     []string{"goose"},
			ConfigDirs:   []string{"~/.config/goose"},
			ProcessNames: []string{"goose"},
		},
		VersionPlistKey: "",
		ClientInfoNames: nil,
		Documents: []Document{{
			URL:         "https://goose-docs.ai/oauth/client-metadata.json",
			DisplayName: "Goose",
			DisplayOnly: false,
			Enabled:     true,
		}},
	},

	// Verified 2026-07, re-verified 2026-08-19 (all four documents fetched
	// live). OpenAI mints CIMD documents per connector and per Codex target
	// server, so those namespaces are unbounded and only patterns can admit
	// them — see the admission package's pattern rules.
	//
	// chatgpt.com is a TEMPLATE endpoint: any id in a wildcard position
	// returns HTTP 200 with a valid self-referential document, so a
	// successful fetch proves nothing about an id being real. The patterns
	// stay safe because the id is never reflected into consent-visible
	// metadata — client_name, client_uri, and logo_uri are constant per
	// product — and the per-id redirect_uris are loopback-only, so minting a
	// document at a chosen id gains an attacker nothing over presenting the
	// vendor's real client_id.
	{
		ID:          "chatgpt-classic",
		VendorKey:   "openai",
		DisplayName: "ChatGPT Classic",
		Category:    CategoryHarness,
		Signatures: Signatures{
			BundleIDs:    []string{"com.openai.chat"},
			Binaries:     nil,
			ConfigDirs:   nil,
			ProcessNames: nil,
		},
		VersionPlistKey: "",
		ClientInfoNames: []string{"ChatGPT", "chatgpt"},
		Documents: []Document{
			{
				// One document per ChatGPT connector, {id} derived from the
				// MCP server origin (SHAKE-256 over the origin, per OpenAI's
				// Apps SDK docs and the Codex CLI source) — deterministic per
				// server, unbounded across servers.
				URL:         "https://chatgpt.com/oauth/*/client.json",
				DisplayName: "ChatGPT (connectors)",
				DisplayOnly: false,
				Enabled:     true,
			},
			{
				// The connector platform's stable shared document. NOT
				// DisplayOnly: the connector wildcard requires exactly one
				// path segment between /oauth/ and /client.json, and this URL
				// has none, so nothing else admits it.
				URL:         "https://chatgpt.com/oauth/client.json",
				DisplayName: "ChatGPT",
				DisplayOnly: false,
				Enabled:     true,
			},
		},
	},
	{
		ID:          "codex",
		VendorKey:   "openai",
		DisplayName: "Codex",
		Category:    CategoryHarness,
		Signatures: Signatures{
			BundleIDs:    nil,
			Binaries:     []string{"codex"},
			ConfigDirs:   []string{"~/.codex"},
			ProcessNames: []string{"codex"},
		},
		VersionPlistKey: "",
		ClientInfoNames: []string{"codex", "codex-cli", "Codex CLI"},
		Documents: []Document{
			{
				// Codex CLI >=0.148.0 (first stable release 2026-08-18,
				// openai/codex PR #38089) mints one document per MCP server:
				// {id} is base64url-no-pad of the first 9 bytes of SHA-256 of
				// the full server URL, computed client-side. Deterministic
				// per server URL, not per install, but the server URL space
				// is unbounded, so only a pattern can admit it.
				URL:         "https://chatgpt.com/oauth/codex/*/client.json",
				DisplayName: "Codex CLI",
				DisplayOnly: false,
				Enabled:     true,
			},
			{
				// The stable shared Codex document. No released Codex version
				// has presented it, but OpenAI's docs say Codex will switch to
				// it for authorization servers advertising RFC 9207.
				// DisplayOnly because the connector wildcard already admits
				// it — which is exactly why naming it here matters: a literal
				// match wins over that wildcard, so a caller presenting it is
				// attributed to Codex rather than to ChatGPT.
				URL:         "https://chatgpt.com/oauth/codex/client.json",
				DisplayName: "Codex CLI (stable document)",
				DisplayOnly: true,
				Enabled:     true,
			},
		},
	},

	// Verified 2026-07. Notion publishes two equally-valid documents on two
	// origins, each self-consistent with its own client_uri and redirect_uris.
	// Seeding only one would reject half of Notion's traffic. The apex
	// (notion.so, no www) 301s to the www document, whose client_id is the www
	// form, so the apex URL is not itself a valid client_id.
	{
		ID:              "notion",
		VendorKey:       "notion",
		DisplayName:     "Notion",
		Category:        "",
		Signatures:      Signatures{BundleIDs: nil, Binaries: nil, ConfigDirs: nil, ProcessNames: nil},
		VersionPlistKey: "",
		ClientInfoNames: nil,
		Documents: []Document{
			{
				URL:         "https://www.notion.so/oauth/mcp-client-metadata.json",
				DisplayName: "Notion",
				DisplayOnly: false,
				Enabled:     true,
			},
			{
				URL:         "https://app.notion.com/oauth/mcp-client-metadata.json",
				DisplayName: "Notion (app.notion.com)",
				DisplayOnly: false,
				Enabled:     true,
			},
		},
	},

	// Verified 2026-07.
	{
		ID:              "mcpjam",
		VendorKey:       "mcpjam",
		DisplayName:     "MCPJam Inspector",
		Category:        "",
		Signatures:      Signatures{BundleIDs: nil, Binaries: nil, ConfigDirs: nil, ProcessNames: nil},
		VersionPlistKey: "",
		ClientInfoNames: nil,
		Documents: []Document{{
			URL:         "https://www.mcpjam.com/.well-known/oauth/client-metadata.json",
			DisplayName: "MCPJam Inspector",
			DisplayOnly: false,
			Enabled:     true,
		}},
	},

	// Verified 2026-07. CIMD shipped in droid 0.148.0 (2026-06-15).
	{
		ID:              "factory-droid",
		VendorKey:       "factory",
		DisplayName:     "Factory Droid",
		Category:        "",
		Signatures:      Signatures{BundleIDs: nil, Binaries: nil, ConfigDirs: nil, ProcessNames: nil},
		VersionPlistKey: "",
		ClientInfoNames: nil,
		Documents: []Document{{
			URL:         "https://api.factory.ai/mcp/oauth-client",
			DisplayName: "Factory Droid",
			DisplayOnly: false,
			Enabled:     true,
		}},
	},

	// Verified 2026-07.
	{
		ID:              "toolhive",
		VendorKey:       "stacklok",
		DisplayName:     "ToolHive",
		Category:        "",
		Signatures:      Signatures{BundleIDs: nil, Binaries: nil, ConfigDirs: nil, ProcessNames: nil},
		VersionPlistKey: "",
		ClientInfoNames: nil,
		Documents: []Document{{
			URL:         "https://toolhive.dev/oauth/client-metadata.json",
			DisplayName: "ToolHive",
			DisplayOnly: false,
			Enabled:     true,
		}},
	},

	// Verified 2026-08-21. Hermes Agent is served from GitHub Pages. Exact
	// matching keeps this scoped to the single document path; the shared
	// github.io apex is not a widening concern.
	//
	// Signatures verified 2026-09-11 against NousResearch/hermes-agent: the
	// install script puts a `hermes` binary on PATH and the agent keeps its
	// state in ~/.hermes ($HERMES_HOME). Windows uses %LOCALAPPDATA%\hermes,
	// which the home-relative config dir does not cover — the binary carries
	// the match there.
	{
		ID:          "hermes-agent",
		VendorKey:   "nousresearch",
		DisplayName: "Hermes Agent",
		Category:    CategoryAssistant,
		Signatures: Signatures{
			BundleIDs:    nil,
			Binaries:     []string{"hermes"},
			ConfigDirs:   []string{"~/.hermes"},
			ProcessNames: []string{"hermes"},
		},
		VersionPlistKey: "",
		ClientInfoNames: nil,
		Documents: []Document{{
			URL:         "https://nousresearch.github.io/hermes-agent/docs/oauth/client-metadata.json",
			DisplayName: "Hermes Agent",
			DisplayOnly: false,
			Enabled:     true,
		}},
	},

	// Verified 2026-08-26. The apex form (skydive.com, no www) 301s to the www
	// document, whose client_id is the www form, so the apex URL is not itself
	// a valid client_id and is deliberately absent.
	{
		ID:              "skydive",
		VendorKey:       "skydive",
		DisplayName:     "Skydive",
		Category:        "",
		Signatures:      Signatures{BundleIDs: nil, Binaries: nil, ConfigDirs: nil, ProcessNames: nil},
		VersionPlistKey: "",
		ClientInfoNames: nil,
		Documents: []Document{{
			URL:         "https://www.skydive.com/api/v1/external-oauth/client-metadata",
			DisplayName: "Skydive",
			DisplayOnly: false,
			Enabled:     true,
		}},
	},

	// Verified 2026-09-10, after a production CIMD admission denial for this
	// client_id. The extra "opencode" path segment does NOT mark a per-server
	// namespace the way OpenAI's does: the shorter /oauth/client.json form and
	// any other segment both 404, so there is exactly one document to admit.
	{
		ID:          "opencode",
		VendorKey:   "opencode",
		DisplayName: "opencode",
		Category:    CategoryHarness,
		Signatures: Signatures{
			BundleIDs:    nil,
			Binaries:     []string{"opencode"},
			ConfigDirs:   []string{"~/.config/opencode"},
			ProcessNames: []string{"opencode"},
		},
		VersionPlistKey: "",
		ClientInfoNames: []string{"opencode"},
		Documents: []Document{{
			URL:         "https://opencode.ai/oauth/opencode/client.json",
			DisplayName: "opencode",
			DisplayOnly: false,
			Enabled:     true,
		}},
	},

	// Products below publish no CIMD document. They register dynamically or
	// do not speak MCP to Gram at all, so an access decision about them is
	// recorded and enforces nothing — the dashboard says so rather than
	// implying the gateway turns them away. They carry reported client names
	// for attribution only.
	{
		ID:          "cursor",
		VendorKey:   "cursor",
		DisplayName: "Cursor",
		Category:    CategoryHarness,
		Signatures: Signatures{
			BundleIDs:    []string{"com.todesktop.230313mzl4w4u92"},
			Binaries:     []string{"cursor"},
			ConfigDirs:   []string{"~/.cursor"},
			ProcessNames: []string{"Cursor"},
		},
		VersionPlistKey: "",
		ClientInfoNames: []string{"Cursor", "cursor", "cursor-vscode"},
		Documents:       nil,
	},
	{
		ID:          "gemini-cli",
		VendorKey:   "google",
		DisplayName: "Gemini CLI",
		Category:    CategoryHarness,
		Signatures: Signatures{
			BundleIDs:    nil,
			Binaries:     []string{"gemini"},
			ConfigDirs:   []string{"~/.gemini"},
			ProcessNames: []string{"gemini"},
		},
		VersionPlistKey: "",
		ClientInfoNames: []string{"gemini-cli", "Gemini CLI"},
		Documents:       nil,
	},
	{
		// Rebranded as Devin Desktop in June 2026 and still writing to
		// ~/.codeium/windsurf, so this entry keeps matching it. The id stays
		// `windsurf` because detections and access decisions are recorded
		// against it; renaming would orphan both. Devin's own CLI and editor
		// extension are a separate product below.
		ID:          "windsurf",
		VendorKey:   "codeium",
		DisplayName: "Windsurf",
		Category:    CategoryHarness,
		Signatures: Signatures{
			BundleIDs:    []string{"com.exafunction.windsurf"},
			Binaries:     []string{"windsurf"},
			ConfigDirs:   []string{"~/.codeium/windsurf"},
			ProcessNames: []string{"Windsurf"},
		},
		VersionPlistKey: "",
		ClientInfoNames: []string{"Windsurf", "windsurf"},
		Documents:       nil,
	},
	{
		ID:          "aider",
		VendorKey:   "aider",
		DisplayName: "Aider",
		Category:    CategoryHarness,
		Signatures: Signatures{
			BundleIDs:    nil,
			Binaries:     []string{"aider"},
			ConfigDirs:   []string{"~/.aider"},
			ProcessNames: []string{"aider"},
		},
		VersionPlistKey: "",
		ClientInfoNames: []string{"aider"},
		Documents:       nil,
	},
	{
		ID:          "openclaw",
		VendorKey:   "openclaw",
		DisplayName: "OpenClaw",
		Category:    CategoryAssistant,
		Signatures: Signatures{
			BundleIDs:    nil,
			Binaries:     []string{"openclaw"},
			ConfigDirs:   []string{"~/.openclaw"},
			ProcessNames: []string{"openclaw"},
		},
		VersionPlistKey: "",
		ClientInfoNames: []string{"openclaw", "OpenClaw"},
		Documents:       nil,
	},

	// Signatures verified 2026-09-11 against charmbracelet/crush: the binary
	// is `crush`, config at ~/.config/crush and state at
	// ~/.local/share/crush. XDG-aware, so both can move; the binary carries
	// the match when they do. No CIMD document published, so a decision
	// about it is recorded but not enforceable.
	{
		ID:          "crush",
		VendorKey:   "charm",
		DisplayName: "Crush",
		Category:    CategoryHarness,
		Signatures: Signatures{
			BundleIDs:    nil,
			Binaries:     []string{"crush"},
			ConfigDirs:   []string{"~/.config/crush"},
			ProcessNames: []string{"crush"},
		},
		VersionPlistKey: "",
		ClientInfoNames: []string{"crush", "Crush"},
		Documents:       nil,
	},

	// Signatures verified 2026-09-11 against QwenLM/qwen-code: the CLI is
	// invoked as `qwen`. Its config directory is referenced as ~/.qwen in the
	// docs but not stated normatively, so only the binary is claimed here —
	// a config dir that turns out to be wrong is a signature that never
	// fires, which is worse than not having one.
	{
		ID:          "qwen-code",
		VendorKey:   "alibaba",
		DisplayName: "Qwen Code",
		Category:    CategoryHarness,
		Signatures: Signatures{
			BundleIDs:    nil,
			Binaries:     []string{"qwen"},
			ConfigDirs:   nil,
			ProcessNames: []string{"qwen"},
		},
		VersionPlistKey: "",
		ClientInfoNames: []string{"qwen-code", "Qwen Code"},
		Documents:       nil,
	},

	// Signatures verified 2026-09-11 against docs.cline.bot: the CLI installs
	// as `cline` via npm. Cline is primarily a VS Code extension, and the
	// extension install is NOT covered here — an extension lives under
	// ~/.vscode/extensions/<publisher>.<name>-<version>, a versioned path the
	// config-dir matcher cannot express. Devices running only the extension
	// go undetected; see the shadow-ai catalog notes.
	{
		ID:          "cline",
		VendorKey:   "cline",
		DisplayName: "Cline",
		Category:    CategoryHarness,
		Signatures: Signatures{
			BundleIDs:    nil,
			Binaries:     []string{"cline"},
			ConfigDirs:   nil,
			ProcessNames: []string{"cline"},
		},
		VersionPlistKey: "",
		ClientInfoNames: []string{"cline", "Cline"},
		Documents:       nil,
	},

	// ---------------------------------------------------------------------
	// Editor-extension agents. Verified 2026-09-11 against the VS Code
	// Marketplace extensionquery API (identifiers and install counts) — these
	// are the most-installed agentic tools there, and until config dirs took
	// a wildcard none of them could be detected at all: an extension lives
	// under a version-stamped directory name that moves every release.
	// ---------------------------------------------------------------------

	// 2.0M installs. A Cline fork, so it ships no CLI of its own.
	{
		ID:          "roo-code",
		VendorKey:   "roocode",
		DisplayName: "Roo Code",
		Category:    CategoryHarness,
		Signatures: Signatures{
			BundleIDs:    nil,
			Binaries:     nil,
			ConfigDirs:   []string{"~/.vscode*/extensions/rooveterinaryinc.roo-cline-*", "~/.cursor/extensions/rooveterinaryinc.roo-cline-*", "~/.windsurf/extensions/rooveterinaryinc.roo-cline-*"},
			ProcessNames: nil,
		},
		VersionPlistKey: "",
		ClientInfoNames: []string{"roo-code", "Roo Code"},
		Documents:       nil,
	},

	// 4.1M installs. The CLI installs as `cn` (npm @continuedev/cli).
	{
		ID:          "continue",
		VendorKey:   "continue",
		DisplayName: "Continue",
		Category:    CategoryHarness,
		Signatures: Signatures{
			BundleIDs:    nil,
			Binaries:     []string{"cn"},
			ConfigDirs:   []string{"~/.vscode*/extensions/continue.continue-*", "~/.cursor/extensions/continue.continue-*", "~/.windsurf/extensions/continue.continue-*"},
			ProcessNames: nil,
		},
		VersionPlistKey: "",
		ClientInfoNames: []string{"continue", "Continue"},
		Documents:       nil,
	},

	// 1.5M installs. The CLI installs as `kilo` and `kilocode`
	// (npm @kilocode/cli). The marketplace id is mixed-case but VS Code
	// lowercases the directory it writes.
	{
		ID:          "kilo-code",
		VendorKey:   "kilocode",
		DisplayName: "Kilo Code",
		Category:    CategoryHarness,
		Signatures: Signatures{
			BundleIDs:    nil,
			Binaries:     []string{"kilo", "kilocode"},
			ConfigDirs:   []string{"~/.vscode*/extensions/kilocode.kilo-code-*", "~/.cursor/extensions/kilocode.kilo-code-*", "~/.windsurf/extensions/kilocode.kilo-code-*"},
			ProcessNames: nil,
		},
		VersionPlistKey: "",
		ClientInfoNames: []string{"kilo-code", "Kilo Code"},
		Documents:       nil,
	},

	// 777k installs. Extension only.
	{
		ID:          "augment",
		VendorKey:   "augment",
		DisplayName: "Augment Code",
		Category:    CategoryHarness,
		Signatures: Signatures{
			BundleIDs:    nil,
			Binaries:     nil,
			ConfigDirs:   []string{"~/.vscode*/extensions/augment.vscode-augment-*", "~/.cursor/extensions/augment.vscode-augment-*", "~/.windsurf/extensions/augment.vscode-augment-*"},
			ProcessNames: nil,
		},
		VersionPlistKey: "",
		ClientInfoNames: []string{"augment", "Augment Code"},
		Documents:       nil,
	},

	// Cognition's cloud agent, which reaches a device through its editor
	// extension (cognition.devin) and its `devin` CLI. Distinct from Devin
	// Desktop, which is the rebranded Windsurf above and matches on
	// ~/.codeium/windsurf.
	{
		ID:          "devin",
		VendorKey:   "cognition",
		DisplayName: "Devin",
		Category:    CategoryHarness,
		Signatures: Signatures{
			BundleIDs:    nil,
			Binaries:     []string{"devin"},
			ConfigDirs:   []string{"~/.vscode*/extensions/cognition.devin-*", "~/.cursor/extensions/cognition.devin-*", "~/.windsurf/extensions/cognition.devin-*"},
			ProcessNames: nil,
		},
		VersionPlistKey: "",
		ClientInfoNames: []string{"devin", "Devin"},
		Documents:       nil,
	},

	// ---------------------------------------------------------------------
	// Terminal and IDE agents with their own install.
	// ---------------------------------------------------------------------

	// Binary verified 2026-09-11 from npm @sourcegraph/amp.
	{
		ID:          "amp",
		VendorKey:   "sourcegraph",
		DisplayName: "Amp",
		Category:    CategoryHarness,
		Signatures: Signatures{
			BundleIDs:    nil,
			Binaries:     []string{"amp"},
			ConfigDirs:   nil,
			ProcessNames: []string{"amp"},
		},
		VersionPlistKey: "",
		ClientInfoNames: []string{"amp", "Amp"},
		Documents:       nil,
	},

	// Binary verified 2026-09-11 from PyPI openhands-ai.
	{
		ID:          "openhands",
		VendorKey:   "allhands",
		DisplayName: "OpenHands",
		Category:    CategoryHarness,
		Signatures: Signatures{
			BundleIDs:    nil,
			Binaries:     []string{"openhands"},
			ConfigDirs:   nil,
			ProcessNames: []string{"openhands"},
		},
		VersionPlistKey: "",
		ClientInfoNames: []string{"openhands", "OpenHands"},
		Documents:       nil,
	},

	// Bundle id and data paths verified 2026-09-11 from the Homebrew cask.
	{
		ID:          "trae",
		VendorKey:   "bytedance",
		DisplayName: "Trae",
		Category:    CategoryHarness,
		Signatures: Signatures{
			BundleIDs:    []string{"com.trae.app"},
			Binaries:     nil,
			ConfigDirs:   []string{"~/.trae", "~/Library/Application Support/Trae"},
			ProcessNames: []string{"Trae"},
		},
		VersionPlistKey: "",
		ClientInfoNames: []string{"trae", "Trae"},
		Documents:       nil,
	},

	// Bundle id and data paths verified 2026-09-11 from the Homebrew cask.
	// An AI terminal rather than a coding agent, but it runs agents and is
	// filed with them for the same reason Goose is.
	{
		ID:          "warp",
		VendorKey:   "warp",
		DisplayName: "Warp",
		Category:    CategoryHarness,
		Signatures: Signatures{
			BundleIDs:    []string{"dev.warp.Warp-Stable"},
			Binaries:     nil,
			ConfigDirs:   []string{"~/.warp"},
			ProcessNames: []string{"Warp"},
		},
		VersionPlistKey: "",
		ClientInfoNames: []string{"warp", "Warp"},
		Documents:       nil,
	},

	// ---------------------------------------------------------------------
	// Assistants: multi-provider chat apps that speak MCP but are not coding
	// tools. Bundle ids and data paths verified 2026-09-11 from Homebrew
	// casks; the two server-shaped ones carry binaries from their package
	// registries instead.
	// ---------------------------------------------------------------------
	{
		ID:          "cherry-studio",
		VendorKey:   "cherrystudio",
		DisplayName: "Cherry Studio",
		Category:    CategoryAssistant,
		Signatures: Signatures{
			BundleIDs:    []string{"com.kangfenmao.CherryStudio"},
			Binaries:     nil,
			ConfigDirs:   []string{"~/Library/Application Support/CherryStudio"},
			ProcessNames: []string{"Cherry Studio"},
		},
		VersionPlistKey: "",
		ClientInfoNames: []string{"cherry-studio", "Cherry Studio"},
		Documents:       nil,
	},
	{
		ID:          "msty",
		VendorKey:   "msty",
		DisplayName: "Msty",
		Category:    CategoryAssistant,
		Signatures: Signatures{
			BundleIDs:    []string{"app.msty.app"},
			Binaries:     nil,
			ConfigDirs:   []string{"~/Library/Application Support/Msty"},
			ProcessNames: []string{"Msty"},
		},
		VersionPlistKey: "",
		ClientInfoNames: []string{"msty", "Msty"},
		Documents:       nil,
	},
	{
		ID:          "anythingllm",
		VendorKey:   "mintplexlabs",
		DisplayName: "AnythingLLM",
		Category:    CategoryAssistant,
		Signatures: Signatures{
			BundleIDs:    nil,
			Binaries:     nil,
			ConfigDirs:   []string{"~/Library/Application Support/anythingllm-desktop"},
			ProcessNames: []string{"AnythingLLM"},
		},
		VersionPlistKey: "",
		ClientInfoNames: []string{"anythingllm", "AnythingLLM"},
		Documents:       nil,
	},

	// Self-hosted web UIs. Installed as packages and reached through a
	// browser, so there is no bundle to match — the console script is the
	// signature. Binaries verified 2026-09-11 from PyPI open-webui and the
	// LibreChat repository.
	{
		ID:          "open-webui",
		VendorKey:   "openwebui",
		DisplayName: "Open WebUI",
		Category:    CategoryAssistant,
		Signatures: Signatures{
			BundleIDs:    nil,
			Binaries:     []string{"open-webui"},
			ConfigDirs:   nil,
			ProcessNames: []string{"open-webui"},
		},
		VersionPlistKey: "",
		ClientInfoNames: []string{"open-webui", "Open WebUI"},
		Documents:       nil,
	},

	// Local model runtimes. They never speak MCP to Gram, so they carry no
	// documents and no reported names: there is no caller to recognize.
	// Bundle id and data path verified 2026-09-11 from the Homebrew cask. Jan
	// is both a runtime and a chat UI; filed as a runtime because that is the
	// job it is compared against Ollama and LM Studio for.
	{
		ID:          "jan",
		VendorKey:   "menlo",
		DisplayName: "Jan",
		Category:    CategoryLocalModel,
		Signatures: Signatures{
			BundleIDs:    []string{"jan.ai.app"},
			Binaries:     nil,
			ConfigDirs:   []string{"~/Library/Application Support/Jan", "~/.config/jan"},
			ProcessNames: []string{"Jan"},
		},
		VersionPlistKey: "",
		ClientInfoNames: nil,
		Documents:       nil,
	},

	// Bundle id and data path verified 2026-09-11 from the Homebrew cask.
	{
		ID:          "gpt4all",
		VendorKey:   "nomic",
		DisplayName: "GPT4All",
		Category:    CategoryLocalModel,
		Signatures: Signatures{
			BundleIDs:    []string{"com.nomic-ai.gpt4all"},
			Binaries:     nil,
			ConfigDirs:   []string{"~/Library/Application Support/GPT4All"},
			ProcessNames: []string{"gpt4all"},
		},
		VersionPlistKey: "",
		ClientInfoNames: nil,
		Documents:       nil,
	},

	// The engine most of the others embed, installed on its own by people
	// running models by hand. Binaries are the upstream console scripts.
	{
		ID:          "llamacpp",
		VendorKey:   "ggml",
		DisplayName: "llama.cpp",
		Category:    CategoryLocalModel,
		Signatures: Signatures{
			BundleIDs:    nil,
			Binaries:     []string{"llama-server", "llama-cli"},
			ConfigDirs:   nil,
			ProcessNames: []string{"llama-server"},
		},
		VersionPlistKey: "",
		ClientInfoNames: nil,
		Documents:       nil,
	},

	// Binary verified 2026-09-11 from PyPI vllm. A serving runtime rather
	// than a desktop app, so it shows up on developer machines and build
	// boxes rather than laptops generally.
	{
		ID:          "vllm",
		VendorKey:   "vllm",
		DisplayName: "vLLM",
		Category:    CategoryLocalModel,
		Signatures: Signatures{
			BundleIDs:    nil,
			Binaries:     []string{"vllm"},
			ConfigDirs:   nil,
			ProcessNames: []string{"vllm"},
		},
		VersionPlistKey: "",
		ClientInfoNames: nil,
		Documents:       nil,
	},

	{
		ID:          "localai",
		VendorKey:   "localai",
		DisplayName: "LocalAI",
		Category:    CategoryLocalModel,
		Signatures: Signatures{
			BundleIDs:    nil,
			Binaries:     []string{"local-ai"},
			ConfigDirs:   nil,
			ProcessNames: []string{"local-ai"},
		},
		VersionPlistKey: "",
		ClientInfoNames: nil,
		Documents:       nil,
	},

	{
		ID:          "koboldcpp",
		VendorKey:   "koboldcpp",
		DisplayName: "KoboldCpp",
		Category:    CategoryLocalModel,
		Signatures: Signatures{
			BundleIDs:    nil,
			Binaries:     []string{"koboldcpp"},
			ConfigDirs:   nil,
			ProcessNames: []string{"koboldcpp"},
		},
		VersionPlistKey: "",
		ClientInfoNames: nil,
		Documents:       nil,
	},

	{
		ID:          "ollama",
		VendorKey:   "ollama",
		DisplayName: "Ollama",
		Category:    CategoryLocalModel,
		Signatures: Signatures{
			BundleIDs:    []string{"com.electron.ollama"},
			Binaries:     []string{"ollama"},
			ConfigDirs:   []string{"~/.ollama"},
			ProcessNames: []string{"ollama"},
		},
		VersionPlistKey: "",
		ClientInfoNames: nil,
		Documents:       nil,
	},
	{
		ID:          "lmstudio",
		VendorKey:   "lmstudio",
		DisplayName: "LM Studio",
		Category:    CategoryLocalModel,
		Signatures: Signatures{
			BundleIDs:    []string{"ai.elementlabs.lmstudio"},
			Binaries:     []string{"lms"},
			ConfigDirs:   []string{"~/.lmstudio"},
			ProcessNames: []string{"LM Studio"},
		},
		VersionPlistKey: "",
		ClientInfoNames: nil,
		Documents:       nil,
	},
}
