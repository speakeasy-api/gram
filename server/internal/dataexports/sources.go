package dataexports

import "fmt"

type dataSource string

var validDataSources = map[dataSource]struct{}{
	"product_telemetry": {},
}

func parseDataSource(value string) (dataSource, error) {
	source := dataSource(value)
	if _, ok := validDataSources[source]; !ok {
		return "", fmt.Errorf("unsupported data source %q", value)
	}
	return source, nil
}
