package mcpconnectionattempts

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// subjectsConn keeps each written row's subject and repeats.
type subjectsConn struct {
	driver.Conn

	mu   sync.Mutex
	rows []writtenSubject
}

type writtenSubject struct {
	subject string
	repeats uint32
}

func (c *subjectsConn) PrepareBatch(context.Context, string, ...driver.PrepareBatchOption) (driver.Batch, error) {
	return &subjectsBatch{Batch: nil, conn: c}, nil
}

func (c *subjectsConn) written() []writtenSubject {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]writtenSubject(nil), c.rows...)
}

type subjectsBatch struct {
	driver.Batch

	conn *subjectsConn
}

func (b *subjectsBatch) Append(values ...any) error {
	const subjectColumn = 22
	subject, ok := values[subjectColumn].(string)
	if !ok {
		return errors.New("subject_urn is column 22")
	}
	repeats, ok := values[len(values)-2].(uint32)
	if !ok {
		return errors.New("repeats is the second last column")
	}
	b.conn.mu.Lock()
	defer b.conn.mu.Unlock()
	b.conn.rows = append(b.conn.rows, writtenSubject{subject: subject, repeats: repeats})
	return nil
}

func (b *subjectsBatch) Send() error { return nil }

func (b *subjectsBatch) Abort() error { return nil }

// A flood of refusals for new subjects of one issuer folds into one key, so it can't fill the history.
func TestOnceLimiter_RefusedSubjectsFoldByKey(t *testing.T) {
	t.Parallel()

	limiter := newOnceLimiter()
	now := time.Now()
	refused := func(fold, subject string) Row {
		row := limiterRow("org", "server", "subject_not_admitted", subject)
		FoldRefusedSubjects(fold).apply(&row)
		return row
	}
	allowed := 0
	for range onceKeyBurst * 3 {
		if limiterAllows(limiter, refused("workload_issuer:a", "workload:a:"+uuid.NewString()), now) {
			allowed++
		}
	}
	require.Equal(t, onceKeyBurst, allowed)
	require.True(t, limiterAllows(limiter, refused("workload_issuer:b", "workload:b:"+uuid.NewString()), now), "another issuer folds apart")

	signedIn := limiterRow("org", "server", "", "workload:a:"+uuid.NewString())
	signedIn.StepResult = ResultSignedIn.String()
	FoldRefusedSubjects("workload_issuer:a").apply(&signedIn)
	require.Empty(t, signedIn.foldSubjects, "a success keeps folding per subject")
	require.True(t, limiterAllows(limiter, signedIn, now))
}

// A row that stands for held-back rows of other subjects names none of them; one that stands
// only for itself keeps its subject, so an admin can still allow it.
func TestRecord_FoldedRefusalsNameNoSubjectOnceTheyCarryRepeats(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		conn := &subjectsConn{Conn: nil, mu: sync.Mutex{}, rows: nil}
		cfg := testConfig()
		cfg.queueSize = 4096
		cfg.repeatsInterval = time.Minute
		recorder := newRecorder(testenv.NewLogger(t), sdkmetric.NewMeterProvider(), conn, logsOn, cfg)
		scope := AuthorizationServerScope("org-test", uuid.Nil, uuid.New(), AppWorkload)
		issuerID := uuid.New()

		const attempts = 40
		for range attempts {
			subject := urn.NewWorkloadSubject(issuerID, uuid.NewString())
			recorder.Once(t.Context(), scope, StepInboundToken, CodeSubjectNotAdmitted, Subject(&subject), FoldRefusedSubjects("workload_issuer:"+issuerID.String()))
		}
		time.Sleep(2 * time.Minute) //nolint:forbidigo // GG013: advances the synctest fake clock inside a synctest.Test bubble
		synctest.Wait()
		require.NoError(t, recorder.Shutdown(t.Context()))

		written := conn.written()
		require.Len(t, written, onceKeyBurst+1)
		total := 0
		for _, row := range written {
			total += 1 + int(row.repeats)
			if row.repeats == 0 {
				require.NotEmpty(t, row.subject, "a row that stands for itself keeps its subject")
			} else {
				require.Empty(t, row.subject, "a row that stands for others names none of them")
			}
		}
		require.Equal(t, attempts, total)
	})
}

// A shared authorization server reaches no one MCP server: its rows name the issuer.
func TestScope_AuthorizationServerNamesTheIssuer(t *testing.T) {
	t.Parallel()

	issuerID := uuid.New()
	row, err := RowAt(Begin(AuthorizationServerScope("org-test", uuid.Nil, issuerID, AppWorkload)), StepInboundToken, CodeSubjectNotAdmitted, true, time.Now())
	require.NoError(t, err)
	require.Equal(t, issuerID.String(), row.ServerID)
	require.Equal(t, uuid.Nil, row.ProjectID)
	require.Equal(t, serverKindAuthorizationServer, row.ServerKind)

	_, err = RowAt(Begin(AuthorizationServerScope("org-test", uuid.Nil, uuid.Nil, AppWorkload)), StepInboundToken, CodeSubjectNotAdmitted, true, time.Now())
	require.Error(t, err, "an authorization server row needs its issuer")
}
