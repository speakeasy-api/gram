package plugins

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
	"sync"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"

	domainskills "github.com/speakeasy-api/gram/server/internal/skills"
)

const (
	agentPluginSchemaID = "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"
	agentMCPSchemaID    = "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json"
	agentPluginRoot     = "agent-plugins"
)

//go:embed agent_plugins_schema/plugin.schema.json
var agentPluginSchemaJSON []byte

//go:embed agent_plugins_schema/mcp.schema.json
var agentMCPSchemaJSON []byte

var ErrAgentPluginsV1Incompatible = errors.New("plugin is not compatible with Agent Plugins 1.0")

type agentPluginCredentialMode uint8

const (
	agentPluginCredentialsPackage agentPluginCredentialMode = iota
	agentPluginCredentialsKeyless
)

type agentPluginCompatibility struct {
	Compatible bool
	Reasons    []string
}

type agentPluginManifest struct {
	Schema      string            `json:"$schema"`
	Name        string            `json:"name"`
	Version     string            `json:"version,omitempty"`
	Description string            `json:"description,omitempty"`
	Author      agentPluginAuthor `json:"author"`
	Homepage    string            `json:"homepage,omitempty"`
}

type agentPluginAuthor struct {
	Name string `json:"name,omitempty"`
	URL  string `json:"url,omitempty"`
}

type agentMCPConfig struct {
	Schema     string                    `json:"$schema"`
	MCPServers map[string]agentMCPServer `json:"mcpServers"`
}

type agentMCPServer struct {
	Type    string            `json:"type"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	CWD     string            `json:"cwd,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

var (
	agentSchemasOnce sync.Once
	agentSchemas     map[string]*jsonschema.Schema
	errAgentSchemas  error
)

const agentPluginNamePattern = `^(?!.*(?:--|\.\.))[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?$`

type agentSchemaRegexp struct {
	pattern string
	re      *regexp.Regexp
	name    bool
}

func (r *agentSchemaRegexp) MatchString(value string) bool {
	if r.name && (strings.Contains(value, "--") || strings.Contains(value, "..")) {
		return false
	}
	return r.re.MatchString(value)
}

func (r *agentSchemaRegexp) String() string { return r.pattern }

func loadAgentPluginSchemas() (map[string]*jsonschema.Schema, error) {
	agentSchemasOnce.Do(func() {
		compiler := jsonschema.NewCompiler()
		compiler.UseRegexpEngine(func(pattern string) (jsonschema.Regexp, error) {
			if pattern == agentPluginNamePattern {
				return &agentSchemaRegexp{pattern: pattern, re: regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?$`), name: true}, nil
			}
			re, err := regexp.Compile(pattern)
			if err != nil {
				return nil, fmt.Errorf("compile schema regexp: %w", err)
			}
			return &agentSchemaRegexp{pattern: pattern, re: re, name: false}, nil
		})
		for id, schemaBytes := range map[string][]byte{
			agentPluginSchemaID: agentPluginSchemaJSON,
			agentMCPSchemaID:    agentMCPSchemaJSON,
		} {
			raw, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaBytes))
			if err != nil {
				errAgentSchemas = fmt.Errorf("parse vendored schema %s: %w", id, err)
				return
			}
			if err := compiler.AddResource(id, raw); err != nil {
				errAgentSchemas = fmt.Errorf("add vendored schema %s: %w", id, err)
				return
			}
		}
		agentSchemas = make(map[string]*jsonschema.Schema, 2)
		for _, id := range []string{agentPluginSchemaID, agentMCPSchemaID} {
			schema, err := compiler.Compile(id)
			if err != nil {
				errAgentSchemas = fmt.Errorf("compile vendored schema %s: %w", id, err)
				return
			}
			agentSchemas[id] = schema
		}
	})
	return agentSchemas, errAgentSchemas
}

func classifyAgentPlugin(p PluginInfo) agentPluginCompatibility {
	reasons := append([]string(nil), p.AgentPluginsV1Issues...)
	seenServers := make(map[string]struct{}, len(p.Servers))
	for _, server := range p.Servers {
		if _, exists := seenServers[server.DisplayName]; exists {
			reasons = append(reasons, "duplicate server name")
		}
		seenServers[server.DisplayName] = struct{}{}
		if !server.IsUnproxied && !server.IsPublic && !server.IsOAuth {
			reasons = append(reasons, "server requires a Gram credential")
		}
		if len(server.EnvConfigs) > 0 {
			reasons = append(reasons, "server requires environment-backed headers")
		}
		if err := validateAgentPluginRemoteURL(server.MCPURL); err != nil {
			reasons = append(reasons, "server URL is not portable")
		}
	}
	for _, skill := range p.Skills {
		if !domainskills.ValidSpecName(skill.Name) {
			reasons = append(reasons, "skill name is invalid")
		}
	}

	manifest := agentPluginManifest{Schema: agentPluginSchemaID, Name: p.Slug, Version: "0.1.0", Description: p.Description, Author: agentPluginAuthor{Name: "", URL: ""}, Homepage: ""}
	if err := validateAgentPluginDocument(agentPluginSchemaID, manifest); err != nil {
		reasons = append(reasons, "plugin manifest is invalid: "+err.Error())
	}
	return agentPluginCompatibility{Compatible: len(reasons) == 0, Reasons: reasons}
}

func validateAgentPluginRemoteURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" {
		return errors.New("url must be absolute and must not contain user information or a fragment")
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme != "http" {
		return errors.New("url scheme must be HTTP or HTTPS")
	}
	host := u.Hostname()
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("non-loopback HTTP URL")
	}
	return nil
}

func validateAgentPluginDocument(schemaID string, document any) error {
	schemas, err := loadAgentPluginSchemas()
	if err != nil {
		return err
	}
	raw, err := json.Marshal(document)
	if err != nil {
		return fmt.Errorf("marshal Agent Plugins document: %w", err)
	}
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("parse Agent Plugins document: %w", err)
	}
	if err := schemas[schemaID].Validate(value); err != nil {
		return fmt.Errorf("validate Agent Plugins document: %w", err)
	}
	return nil
}

func compileAgentPlugin(p PluginInfo, cfg GenerateConfig, mode agentPluginCredentialMode) (map[string][]byte, error) {
	if compatibility := classifyAgentPlugin(p); !compatibility.Compatible {
		return nil, fmt.Errorf("%w: %s", ErrAgentPluginsV1Incompatible, strings.Join(compatibility.Reasons, ", "))
	}
	cfg.APIKey = ""
	cfg.HooksAPIKey = ""

	files := make(map[string][]byte)
	manifest := agentPluginManifest{
		Schema:      agentPluginSchemaID,
		Name:        p.Slug,
		Version:     pluginManifestVersion(cfg),
		Description: p.Description,
		Author:      agentPluginAuthor{Name: cfg.OrgName, URL: "https://www.speakeasy.com/"},
		Homepage:    "https://www.speakeasy.com/product/gram",
	}
	manifestJSON, err := marshalJSON(manifest)
	if err != nil {
		return nil, fmt.Errorf("marshal Agent Plugins plugin.json: %w", err)
	}
	files["plugin.json"] = manifestJSON

	mcpServers := make(map[string]agentMCPServer, len(p.Servers))
	for _, server := range p.Servers {
		mcpServers[server.DisplayName] = agentMCPServer{Type: "streamable-http", Command: "", Args: nil, Env: nil, CWD: "", URL: server.MCPURL, Headers: nil}
	}
	mcpConfig := agentMCPConfig{Schema: agentMCPSchemaID, MCPServers: mcpServers}
	mcpJSON, err := marshalJSON(mcpConfig)
	if err != nil {
		return nil, fmt.Errorf("marshal Agent Plugins mcp.json: %w", err)
	}
	files["mcp.json"] = mcpJSON
	emitPluginSkills(files, "", p)

	if mode == agentPluginCredentialsPackage {
		cursorJSON, err := marshalJSON(cursorPluginMeta{Name: p.Slug + "-cursor", DisplayName: p.Name + " (Cursor)", Description: p.Description, Version: pluginManifestVersion(cfg), Author: cursorAuthor{Name: cfg.OrgName, Email: ""}, Homepage: "https://getgram.ai"})
		if err != nil {
			return nil, fmt.Errorf("marshal shared Cursor plugin.json: %w", err)
		}
		files[".cursor-plugin/plugin.json"] = cursorJSON

		legacyCodexFiles := make(map[string][]byte)
		if err := generateCodexPluginInDir(legacyCodexFiles, "", p.Slug+"-codex", p, cfg); err != nil {
			return nil, fmt.Errorf("generate shared Codex legacy overlay: %w", err)
		}
		files[".codex-plugin/plugin.json"] = legacyCodexFiles[".codex-plugin/plugin.json"]
		files[".mcp.json"] = legacyCodexFiles[".mcp.json"]
	}

	if err := validateAgentPluginDocument(agentPluginSchemaID, manifest); err != nil {
		return nil, err
	}
	if err := validateAgentPluginDocument(agentMCPSchemaID, mcpConfig); err != nil {
		return nil, err
	}
	for filePath := range files {
		if strings.HasPrefix(filePath, "/") || strings.HasPrefix(filePath, "../") || strings.Contains(filePath, "/../") {
			return nil, fmt.Errorf("agent plugins package path escapes root: %s", filePath)
		}
	}
	return files, nil
}
