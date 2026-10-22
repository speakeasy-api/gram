package assistants

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Kit v0.2.17 only upserts client MCP servers. Its session/close checkpoints
// history and releases all resident connections; cold session/resume constructs
// a new isolated MCP set. Never use session/delete: conversation IDs and the
// control plane's transcript correlation must survive credential rotation.
func (p *KitRuntimeProtocol) closeForMCPReplacement(ctx context.Context, r assistantRuntimeRecord, session string) error {
	c, err := p.dial(ctx, r)
	if err != nil {
		return err
	}
	defer closeKitTransport(c)
	// Resume may replay more messages than the transport queue. This temporary
	// controller drains them; the real turn replays and projects durable history.
	drainCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		for {
			select {
			case <-drainCtx.Done():
				return
			case _, ok := <-c.Notifications():
				if !ok {
					return
				}
			}
		}
	}()
	state := func() (string, error) {
		raw, err := c.Call(ctx, "session/list", map[string]any{}, "")
		if err != nil {
			return "", fmt.Errorf("list Kit sessions for MCP replacement: %w", err)
		}
		inventory, err := kitCheckpointInventory(raw)
		if err != nil {
			return "", err
		}
		row, ok := inventory[session]
		if !ok || row.CWD != p.config.CWD {
			return "", ErrKitRuntimeBinding
		}
		return row.State, nil
	}
	initial, err := state()
	if err != nil {
		return err
	}
	if initial == "restorable" {
		return nil
	}
	if err := c.AttachSession(ctx, session); err != nil {
		return fmt.Errorf("attach Kit for MCP replacement: %w", err)
	}
	if _, err := c.Call(ctx, "session/resume", map[string]any{"sessionId": session, "cwd": p.config.CWD, "replayFrom": map[string]string{"type": "start"}}, session); err != nil {
		return fmt.Errorf("resume Kit for MCP replacement: %w", err)
	}
	raw, err := c.Call(ctx, "kit/session/status", map[string]string{"sessionId": session}, session)
	if err != nil {
		return fmt.Errorf("observe Kit before MCP replacement: %w", err)
	}
	settled, err := kitSettled(raw, session)
	if err != nil {
		return err
	}
	if !settled {
		return errors.New("kit session must settle before replacing MCP connections")
	}
	if _, err := c.Call(ctx, "session/close", map[string]string{"sessionId": session}, session); err != nil {
		return fmt.Errorf("checkpoint Kit before MCP replacement: %w", err)
	}
	// The close response follows child cleanup; HTTP controller detach can still
	// be queued. Observe released residency before issuing cold session/resume.
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		current, err := state()
		if err != nil {
			return err
		}
		if current == "restorable" {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for Kit residency release: %w", ctx.Err())
		case <-deadline.C:
			return errors.New("kit closed session did not release residency")
		case <-tick.C:
		}
	}

}

// Match the default runner's TokenRegistry: the legitimately minted turn
// credential replaces Authorization, preserving every other server header.
func kitTurnMCPServers(req runTurnRequest) []map[string]any {
	servers := make([]map[string]any, 0, len(req.MCPServers))
	for _, server := range req.MCPServers {
		headers := make([]map[string]string, 0, len(server.Headers)+1)
		for name, value := range server.Headers {
			if req.AuthToken != "" && strings.EqualFold(name, "Authorization") {
				continue
			}
			headers = append(headers, map[string]string{"name": name, "value": value})
		}
		if req.AuthToken != "" {
			headers = append(headers, map[string]string{"name": "Authorization", "value": "Bearer " + req.AuthToken})
		}
		servers = append(servers, map[string]any{"type": "http", "name": server.ID, "url": server.URL, "headers": headers})
	}
	return servers
}
