module github.com/speakeasy-api/gram

go 1.24.4

tool (
	github.com/GoogleCloudPlatform/cloudsql-proxy/cmd/cloud_sql_proxy
	github.com/sqlc-dev/sqlc/cmd/sqlc
	goa.design/goa/v3/cmd/goa
	gotest.tools/gotestsum
)

require (
	cloud.google.com/go/storage v1.55.0
	github.com/KimMachineGun/automemlimit v0.7.3
	github.com/Masterminds/semver/v3 v3.3.1
	github.com/cbroglie/mustache v1.4.0
	github.com/charmbracelet/log v0.4.2
	github.com/ettle/strcase v0.2.0
	github.com/go-logr/logr v1.4.3
	github.com/go-redis/cache/v9 v9.0.0
	github.com/gomarkdown/markdown v0.0.0-20250311123330-531bef5e742b
	github.com/hashicorp/go-cleanhttp v0.5.2
	github.com/jackc/pgerrcode v0.0.0-20240316143900-6e2875d9b438
	github.com/microcosm-cc/bluemonday v1.0.27
	github.com/pb33f/libopenapi v0.22.3
	github.com/pgx-contrib/pgxotel v0.0.0-20250326222047-55ccee468e10
	github.com/posthog/posthog-go v1.5.12
	github.com/redis/go-redis/extra/redisotel/v9 v9.10.0
	github.com/redis/go-redis/v9 v9.10.0
	github.com/samber/lo v1.51.0
	github.com/samber/slog-multi v1.4.0
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.2
	github.com/sourcegraph/conc v0.3.0
	github.com/testcontainers/testcontainers-go v0.37.0
	github.com/testcontainers/testcontainers-go/modules/postgres v0.37.0
	github.com/testcontainers/testcontainers-go/modules/redis v0.37.0
	github.com/urfave/cli/v2 v2.27.7
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.62.0
	go.opentelemetry.io/contrib/instrumentation/runtime v0.62.0
	go.opentelemetry.io/otel v1.37.0
	go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc v1.36.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.36.0
	go.opentelemetry.io/otel/metric v1.37.0
	go.opentelemetry.io/otel/sdk v1.37.0
	go.opentelemetry.io/otel/sdk/metric v1.37.0
	go.opentelemetry.io/otel/trace v1.37.0
	go.temporal.io/api v1.50.0
	go.temporal.io/sdk v1.34.0
	go.temporal.io/sdk/contrib/opentelemetry v0.6.0
	go.uber.org/automaxprocs v1.6.0
	goa.design/clue v1.2.1
	goa.design/goa/v3 v3.21.1
	goa.design/plugins/v3 v3.21.1
	k8s.io/api v0.33.2
	k8s.io/apimachinery v0.33.2
	k8s.io/client-go v0.33.2
)

require (
	cloud.google.com/go v0.121.1 // indirect
	cloud.google.com/go/auth v0.16.1 // indirect
	cloud.google.com/go/auth/oauth2adapt v0.2.8 // indirect
	cloud.google.com/go/compute/metadata v0.7.0 // indirect
	cloud.google.com/go/iam v1.5.2 // indirect
	cloud.google.com/go/monitoring v1.24.2 // indirect
	dario.cat/mergo v1.0.1 // indirect
	github.com/Azure/go-ansiterm v0.0.0-20250102033503-faa5f7b0171c // indirect
	github.com/GoogleCloudPlatform/cloudsql-proxy v1.37.6 // indirect
	github.com/GoogleCloudPlatform/opentelemetry-operations-go/detectors/gcp v1.27.0 // indirect
	github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/metric v0.51.0 // indirect
	github.com/GoogleCloudPlatform/opentelemetry-operations-go/internal/resourcemapping v0.51.0 // indirect
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/aws/smithy-go v1.22.3 // indirect
	github.com/aymerick/douceur v0.2.0 // indirect
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/bitfield/gotestdox v0.2.2 // indirect
	github.com/buger/jsonparser v1.1.1 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cenkalti/backoff/v5 v5.0.2 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/charmbracelet/colorprofile v0.2.3-0.20250311203215-f60798e515dc // indirect
	github.com/charmbracelet/x/ansi v0.8.0 // indirect
	github.com/charmbracelet/x/cellbuf v0.0.13-0.20250311204145-2c3ea96c31dd // indirect
	github.com/charmbracelet/x/term v0.2.1 // indirect
	github.com/cncf/xds/go v0.0.0-20250326154945-ae57f3c0d45f // indirect
	github.com/containerd/log v0.1.0 // indirect
	github.com/containerd/platforms v0.2.1 // indirect
	github.com/coreos/go-systemd/v22 v22.5.0 // indirect
	github.com/cpuguy83/dockercfg v0.3.2 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/dnephin/pflag v1.0.7 // indirect
	github.com/docker/docker v28.1.1+incompatible // indirect
	github.com/docker/go-connections v0.5.0 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/ebitengine/purego v0.8.2 // indirect
	github.com/emicklei/go-restful/v3 v3.11.0 // indirect
	github.com/envoyproxy/go-control-plane/envoy v1.32.4 // indirect
	github.com/envoyproxy/protoc-gen-validate v1.2.1 // indirect
	github.com/facebookgo/clock v0.0.0-20150410010913-600d898af40a // indirect
	github.com/fatih/color v1.17.0 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fsnotify/fsnotify v1.8.0 // indirect
	github.com/fxamacker/cbor/v2 v2.7.0 // indirect
	github.com/go-jose/go-jose/v4 v4.0.5 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/go-openapi/jsonpointer v0.21.0 // indirect
	github.com/go-openapi/jsonreference v0.20.2 // indirect
	github.com/go-openapi/swag v0.23.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/gohugoio/hashstructure v0.5.0 // indirect
	github.com/golang/mock v1.6.0 // indirect
	github.com/google/gnostic-models v0.6.9 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/s2a-go v0.1.9 // indirect
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.6 // indirect
	github.com/googleapis/gax-go/v2 v2.14.2 // indirect
	github.com/gorilla/css v1.0.1 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware v1.4.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.26.3 // indirect
	github.com/hanwen/go-fuse/v2 v2.7.2 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/compress v1.18.0 // indirect
	github.com/lufia/plan9stats v0.0.0-20250317134145-8bc96cf8fc35 // indirect
	github.com/magiconair/properties v1.8.10 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-runewidth v0.0.16 // indirect
	github.com/mdelapenya/tlscert v0.2.0 // indirect
	github.com/moby/docker-image-spec v1.3.1 // indirect
	github.com/moby/go-archive v0.1.0 // indirect
	github.com/moby/patternmatcher v0.6.0 // indirect
	github.com/moby/sys/atomicwriter v0.1.0 // indirect
	github.com/moby/sys/sequential v0.6.0 // indirect
	github.com/moby/sys/user v0.4.0 // indirect
	github.com/moby/sys/userns v0.1.0 // indirect
	github.com/moby/term v0.5.2 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/morikuni/aec v1.0.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/nexus-rpc/sdk-go v0.3.0 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.1 // indirect
	github.com/pganalyze/pg_query_go/v6 v6.1.0 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/planetscale/vtprotobuf v0.6.1-0.20240319094008-0393e58bdf10 // indirect
	github.com/power-devops/perfstat v0.0.0-20240221224432-82ca36839d55 // indirect
	github.com/redis/go-redis/extra/rediscmd/v9 v9.10.0 // indirect
	github.com/robfig/cron v1.2.0 // indirect
	github.com/shirou/gopsutil/v4 v4.25.3 // indirect
	github.com/sirupsen/logrus v1.9.3 // indirect
	github.com/speakeasy-api/jsonpath v0.6.2 // indirect
	github.com/spiffe/go-spiffe/v2 v2.5.0 // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	github.com/tklauser/go-sysconf v0.3.15 // indirect
	github.com/tklauser/numcpus v0.10.0 // indirect
	github.com/vmihailenco/go-tinylfu v0.2.2 // indirect
	github.com/vmihailenco/msgpack/v5 v5.3.4 // indirect
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
	github.com/wk8/go-ordered-map/v2 v2.1.9-0.20240815153524-6ea36470d1bd // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	github.com/zeebo/errs v1.4.0 // indirect
	go.opentelemetry.io/auto/sdk v1.1.0 // indirect
	go.opentelemetry.io/contrib/detectors/gcp v1.36.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.60.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp v1.36.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.36.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.35.0 // indirect
	go.opentelemetry.io/proto/otlp v1.6.0 // indirect
	golang.org/x/oauth2 v0.30.0 // indirect
	golang.org/x/term v0.32.0 // indirect
	golang.org/x/time v0.11.0 // indirect
	google.golang.org/api v0.235.0 // indirect
	google.golang.org/genproto v0.0.0-20250505200425-f936aa4a68b2 // indirect
	gopkg.in/evanphx/json-patch.v4 v4.12.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gotest.tools/gotestsum v1.12.1 // indirect
	k8s.io/klog/v2 v2.130.1 // indirect
	k8s.io/kube-openapi v0.0.0-20250318190949-c8a335a9a2ff // indirect
	k8s.io/utils v0.0.0-20241104100929-3ea5e8cea738 // indirect
	sigs.k8s.io/json v0.0.0-20241010143419-9aa6b5e7a4b3 // indirect
	sigs.k8s.io/randfill v1.0.0 // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.6.0 // indirect
	sigs.k8s.io/yaml v1.4.0 // indirect
)

require (
	cel.dev/expr v0.23.0 // indirect
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/antlr4-go/antlr/v4 v4.13.1 // indirect
	github.com/aymanbagabas/go-osc52/v2 v2.0.1 // indirect
	github.com/charmbracelet/lipgloss v1.1.0 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.7 // indirect
	github.com/cubicdaiya/gonp v1.0.4 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/dimfeld/httppath v0.0.0-20170720192232-ee938bf73598 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/fatih/structtag v1.2.0 // indirect
	github.com/go-chi/chi/v5 v5.2.2
	github.com/go-logfmt/logfmt v0.6.0 // indirect
	github.com/go-sql-driver/mysql v1.9.2 // indirect
	github.com/google/cel-go v0.24.1 // indirect
	github.com/google/uuid v1.6.0
	github.com/gorilla/websocket v1.5.4-0.20250319132907-e064f32e3674 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/pgx/v5 v5.7.5
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/lucasb-eyer/go-colorful v1.2.0 // indirect
	github.com/manveru/faker v0.0.0-20171103152722-9fbc68a78c4d // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/muesli/termenv v0.16.0 // indirect
	github.com/ncruces/go-strftime v0.1.9 // indirect
	github.com/pbnjay/memory v0.0.0-20210728143218-7b4eea64cf58 // indirect
	github.com/pingcap/errors v0.11.5-0.20240311024730-e056997136bb // indirect
	github.com/pingcap/failpoint v0.0.0-20240528011301-b51a646c7c86 // indirect
	github.com/pingcap/log v1.1.0 // indirect
	github.com/pingcap/tidb/pkg/parser v0.0.0-20250324122243-d51e00e5bbf0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/riza-io/grpc-go v0.2.0 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/spf13/cobra v1.9.1 // indirect
	github.com/spf13/pflag v1.0.6 // indirect
	github.com/sqlc-dev/sqlc v1.29.0 // indirect
	github.com/stoewer/go-strcase v1.2.0 // indirect
	github.com/stretchr/testify v1.10.0
	github.com/tetratelabs/wazero v1.9.0 // indirect
	github.com/wasilibs/go-pgquery v0.0.0-20250409022910-10ac41983c07 // indirect
	github.com/wasilibs/wazero-helpers v0.0.0-20240620070341-3dff1577cd52 // indirect
	github.com/xrash/smetrics v0.0.0-20240521201337-686a1a2994c1 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.27.0 // indirect
	golang.org/x/crypto v0.39.0 // indirect
	golang.org/x/exp v0.0.0-20250305212735-054e65f0b394 // indirect
	golang.org/x/mod v0.25.0 // indirect
	golang.org/x/net v0.41.0 // indirect
	golang.org/x/sync v0.15.0
	golang.org/x/sys v0.33.0 // indirect
	golang.org/x/text v0.26.0 // indirect
	golang.org/x/tools v0.34.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20250519155744-55703ea1f237 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250519155744-55703ea1f237 // indirect
	google.golang.org/grpc v1.73.0 // indirect
	google.golang.org/protobuf v1.36.6 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/yaml.v3 v3.0.1
	modernc.org/libc v1.62.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.9.1 // indirect
	modernc.org/sqlite v1.37.0 // indirect
)
