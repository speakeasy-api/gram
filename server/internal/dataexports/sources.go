package dataexports

import "fmt"

type dataSource string

var validDataSources = map[string]struct{}{
	"product_telemetry": {},
}

func parseDataSource(value string) (dataSource, error) {
	if _, ok := validDataSources[value]; !ok {
		return "", fmt.Errorf("unsupported data source %q", value)
	}
	return dataSource(value), nil
}
