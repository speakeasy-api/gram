package dataexports

import "fmt"

const (
	DataSourceProductTelemetry = "product_telemetry"
	DataSourceRiskFindings     = "risk_findings"
)

var validDataSources = map[string]struct{}{
	DataSourceProductTelemetry: {},
	DataSourceRiskFindings:     {},
}

// NormalizeDataSource validates and returns a supported data export source.
func NormalizeDataSource(value string) (string, error) {
	if _, ok := validDataSources[value]; !ok {
		return "", fmt.Errorf("unsupported data source %q", value)
	}
	return value, nil
}
