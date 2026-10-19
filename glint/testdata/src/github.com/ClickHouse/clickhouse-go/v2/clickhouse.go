package clickhouse

// Settings, QueryOption, WithSettings, and Options are minimal stand-ins for
// the github.com/ClickHouse/clickhouse-go/v2 API the noqueryskipasyncinsertwait
// analyzer matches.
type Settings map[string]any

type QueryOptions struct {
	settings Settings
}

type QueryOption func(*QueryOptions) error

func WithSettings(settings Settings) QueryOption {
	return func(o *QueryOptions) error {
		o.settings = settings
		return nil
	}
}

type Options struct {
	Settings Settings
}
