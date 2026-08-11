package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"reflect"
	"strings"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const (
	deviceAgentConfigurationSchemaVersion = 1
	maxDeviceAgentConfigurationBytes      = 64 * 1024
	minSyncIntervalSeconds                = 60
	maxSyncIntervalSeconds                = 24 * 60 * 60
)

var forbiddenDeviceAgentConfigurationKeys = map[string]struct{}{
	"email":     {},
	"org_name":  {},
	"org_slug":  {},
	"org_token": {},
	"v":         {},
}

// knownDeviceAgentConfigurationKeys are the version-1 settings this server
// edits. Update requests replace these wholesale — omitting one removes it —
// while any other stored key is preserved so a server that predates a setting
// cannot delete it on behalf of a client that never saw it.
var knownDeviceAgentConfigurationKeys = map[string]struct{}{
	"platforms":             {},
	"update_channel":        {},
	"auto_update":           {},
	"pinned_target":         {},
	"blocked_versions":      {},
	"sync_interval_seconds": {},
}

// platformAdminOnlyDeviceAgentConfigurationKeys are Speakeasy-internal
// release controls: org admins cannot set them, and updates from org admins
// leave any stored values untouched.
var platformAdminOnlyDeviceAgentConfigurationKeys = []string{
	"update_channel",
	"blocked_versions",
}

// replaceableDeviceAgentConfigurationKeys returns the known keys an update
// from this caller replaces wholesale. Platform-admin-only keys are excluded
// for org admins so their updates preserve stored values instead of removing
// them by omission.
func replaceableDeviceAgentConfigurationKeys(platformAdmin bool) map[string]struct{} {
	if platformAdmin {
		return knownDeviceAgentConfigurationKeys
	}
	keys := make(map[string]struct{}, len(knownDeviceAgentConfigurationKeys))
	for key := range knownDeviceAgentConfigurationKeys {
		keys[key] = struct{}{}
	}
	for _, key := range platformAdminOnlyDeviceAgentConfigurationKeys {
		delete(keys, key)
	}
	return keys
}

// mergeStoredDeviceAgentConfiguration overlays an update on the stored
// document: replaceable keys come only from the request (absence removes
// them), every other stored key survives unless the request overwrites it.
func mergeStoredDeviceAgentConfiguration(incoming map[string]any, stored []byte, replaceable map[string]struct{}) (map[string]any, error) {
	var storedConfig map[string]any
	if err := json.Unmarshal(stored, &storedConfig); err != nil {
		return nil, fmt.Errorf("decode stored device agent configuration: %w", err)
	}

	merged := make(map[string]any, len(storedConfig)+len(incoming))
	for key, value := range storedConfig {
		if _, replaced := replaceable[key]; replaced {
			continue
		}
		merged[key] = value
	}
	maps.Copy(merged, incoming)
	return merged, nil
}

func validateDeviceAgentConfiguration(config map[string]any) ([]byte, error) {
	for key := range config {
		if _, forbidden := forbiddenDeviceAgentConfigurationKeys[key]; forbidden {
			return nil, oops.E(oops.CodeInvalid, nil, "%s is device-local and cannot be configured remotely", key)
		}
	}

	if platforms, ok := config["platforms"]; ok {
		platformMap, ok := platforms.(map[string]any)
		if !ok {
			return nil, oops.E(oops.CodeInvalid, nil, "platforms must be an object")
		}
		for platform, layer := range platformMap {
			if strings.TrimSpace(platform) == "" || len(platform) > 64 {
				return nil, oops.E(oops.CodeInvalid, nil, "platform names must be between 1 and 64 characters")
			}
			switch value := layer.(type) {
			case bool:
				if value {
					return nil, oops.E(oops.CodeInvalid, nil, "platform %s must use false, user, or managed", platform)
				}
			case string:
				if value != "user" && value != "managed" {
					return nil, oops.E(oops.CodeInvalid, nil, "platform %s must use false, user, or managed", platform)
				}
			default:
				return nil, oops.E(oops.CodeInvalid, nil, "platform %s must use false, user, or managed", platform)
			}
		}
	}

	for _, key := range []string{"update_channel", "auto_update", "pinned_target"} {
		if value, ok := config[key]; ok {
			text, ok := value.(string)
			if !ok || strings.TrimSpace(text) == "" || len(text) > 128 {
				return nil, oops.E(oops.CodeInvalid, nil, "%s must be a non-empty string of at most 128 characters", key)
			}
		}
	}

	if value, ok := config["sync_interval_seconds"]; ok {
		seconds, valid := integerValue(value)
		if !valid || seconds < minSyncIntervalSeconds || seconds > maxSyncIntervalSeconds {
			return nil, oops.E(
				oops.CodeInvalid,
				nil,
				"sync_interval_seconds must be a whole number between %d and %d",
				minSyncIntervalSeconds,
				maxSyncIntervalSeconds,
			)
		}
	}

	if value, ok := config["blocked_versions"]; ok {
		versions, ok := value.([]any)
		if !ok {
			return nil, oops.E(oops.CodeInvalid, nil, "blocked_versions must be an array of strings")
		}
		if len(versions) > 100 {
			return nil, oops.E(oops.CodeInvalid, nil, "blocked_versions cannot contain more than 100 entries")
		}
		for _, version := range versions {
			text, ok := version.(string)
			if !ok || strings.TrimSpace(text) == "" || len(text) > 128 {
				return nil, oops.E(oops.CodeInvalid, nil, "blocked_versions must contain non-empty strings of at most 128 characters")
			}
		}
	}

	data, err := json.Marshal(config)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "configuration must be valid JSON")
	}
	if len(data) > maxDeviceAgentConfigurationBytes {
		return nil, oops.E(oops.CodeInvalid, nil, "configuration cannot exceed %d bytes", maxDeviceAgentConfigurationBytes)
	}

	return data, nil
}

func integerValue(value any) (int64, bool) {
	switch number := value.(type) {
	case int:
		return int64(number), true
	case int8:
		return int64(number), true
	case int16:
		return int64(number), true
	case int32:
		return int64(number), true
	case int64:
		return number, true
	case uint:
		if uint64(number) > math.MaxInt64 {
			return 0, false
		}
		return int64(number), true
	case uint8:
		return int64(number), true
	case uint16:
		return int64(number), true
	case uint32:
		return int64(number), true
	case uint64:
		if number > math.MaxInt64 {
			return 0, false
		}
		return int64(number), true
	case float32:
		value := float64(number)
		return int64(value), !math.IsNaN(value) && !math.IsInf(value, 0) && value == math.Trunc(value)
	case float64:
		return int64(number), !math.IsNaN(number) && !math.IsInf(number, 0) && number == math.Trunc(number)
	case json.Number:
		parsed, err := number.Int64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func buildDeviceAgentConfigurationView(row repo.DeviceAgentConfiguration) (*gen.DeviceAgentConfiguration, error) {
	var config map[string]any
	if err := json.Unmarshal(row.Config, &config); err != nil {
		return nil, fmt.Errorf("decode stored device agent configuration: %w", err)
	}

	updatedAt := row.UpdatedAt.Time.UTC().Format(time.RFC3339)
	return &gen.DeviceAgentConfiguration{
		SchemaVersion: int(row.SchemaVersion),
		Config:        config,
		IsConfigured:  true,
		Etag:          deviceAgentConfigurationETag(row.SchemaVersion, row.Config),
		UpdatedAt:     &updatedAt,
	}, nil
}

func buildDeviceAgentConfigurationSnapshot(row repo.DeviceAgentConfiguration) (audit.DeviceAgentConfigurationSnapshot, error) {
	var config map[string]any
	if err := json.Unmarshal(row.Config, &config); err != nil {
		return audit.DeviceAgentConfigurationSnapshot{}, fmt.Errorf("decode stored device agent configuration: %w", err)
	}
	return audit.DeviceAgentConfigurationSnapshot{
		SchemaVersion: row.SchemaVersion,
		Config:        config,
	}, nil
}

func defaultDeviceAgentConfigurationView() *gen.DeviceAgentConfiguration {
	return &gen.DeviceAgentConfiguration{
		SchemaVersion: deviceAgentConfigurationSchemaVersion,
		Config:        map[string]any{},
		IsConfigured:  false,
		Etag:          deviceAgentConfigurationETag(deviceAgentConfigurationSchemaVersion, nil),
		UpdatedAt:     nil,
	}
}

func deviceAgentConfigurationETag(schemaVersion int32, config []byte) string {
	hash := sha256.New()
	_, _ = fmt.Fprintf(hash, "schema=%d\n", schemaVersion)
	_, _ = hash.Write(config)
	return hex.EncodeToString(hash.Sum(nil))
}

func attachDeviceAgentConfiguration(result *gen.GetPluginsResult, configuration *gen.DeviceAgentConfiguration) {
	result.Configuration = configuration
	hash := sha256.New()
	_, _ = fmt.Fprintf(hash, "plugins=%s\nconfiguration=%s\n", result.Etag, configuration.Etag)
	result.Etag = hex.EncodeToString(hash.Sum(nil))
}

// Any payloads decoded by Goa use map[string]any and []any. Direct service tests
// may use typed slices, so normalize those before validation without weakening
// the HTTP contract.
func normalizeDeviceAgentConfiguration(config map[string]any) map[string]any {
	normalized := make(map[string]any, len(config))
	for key, value := range config {
		rv := reflect.ValueOf(value)
		if rv.IsValid() && rv.Kind() == reflect.Slice {
			items := make([]any, rv.Len())
			for i := range rv.Len() {
				items[i] = rv.Index(i).Interface()
			}
			normalized[key] = items
			continue
		}
		normalized[key] = value
	}
	return normalized
}
