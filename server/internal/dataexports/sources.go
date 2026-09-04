package dataexports

import "fmt"

const (
	DataSourceProductTelemetry = "product_telemetry"
	DataSourceRiskFindings     = "risk_findings"
)

type dataSource string

var validDataSources = map[dataSource]struct{}{
	DataSourceProductTelemetry: {},
	DataSourceRiskFindings:     {},
}

func parseDataSource(value string) (dataSource, error) {
	source := dataSource(value)
	if _, ok := validDataSources[source]; !ok {
		return "", fmt.Errorf("unsupported data source %q", value)
	}
	return source, nil
}
