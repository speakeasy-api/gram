package drafts_test

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"testing"

	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/corpus/drafts"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{
		Postgres: true,
		Redis:    true,
	})
	if err != nil {
		log.Fatalf("launch test infra: %v", err)
		os.Exit(1)
	}

	infra = res
	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infra: %v", err)
		os.Exit(1)
	}

	os.Exit(code)
}

type testInstance struct {
	svc       *drafts.Service
	conn      *pgxpool.Pool
	git       *testGitRepo
	projectID uuid.UUID
	orgID     string
}

func newTestService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, "corpus_drafts")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	billingClient := billing.NewStubClient(logger, tracerProvider)
	sessionManager := testenv.NewTestManager(t, logger, conn, redisClient, cache.Suffix("gram-local"), billingClient)

	ctx := testenv.InitAuthContext(t, t.Context(), conn, sessionManager)

	authctx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authctx.ProjectID)

	gitRepo := newTestGitRepo(t, t.TempDir())
	svc := drafts.NewService(conn, gitRepo, drafts.NewMutexWriteLock())

	return ctx, &testInstance{
		svc:       svc,
		conn:      conn,
		git:       gitRepo,
		projectID: *authctx.ProjectID,
		orgID:     authctx.ActiveOrganizationID,
	}
}

// seedGitFile commits a single file, preserving existing files.
func (ti *testInstance) seedGitFile(t *testing.T, path string, content []byte) {
	t.Helper()
	files := map[string][]byte{path: content}

	// Preserve existing files if any
	if existing, err := ti.git.readAllFiles("HEAD"); err == nil {
		for k, v := range existing {
			if _, ok := files[k]; !ok {
				files[k] = v
			}
		}
	}

	_, err := ti.git.CommitFiles(files, "seed: "+path)
	require.NoError(t, err)
}

type testGitRepo struct {
	repo *gogit.Repository
}

func newTestGitRepo(t *testing.T, dir string) *testGitRepo {
	t.Helper()
	r, err := gogit.PlainInit(dir, true)
	require.NoError(t, err)
	return &testGitRepo{repo: r}
}

func (r *testGitRepo) CommitFiles(files map[string][]byte, message string) (string, error) {
	storer := r.repo.Storer

	var entries []object.TreeEntry
	for path, content := range files {
		obj := storer.NewEncodedObject()
		obj.SetType(plumbing.BlobObject)
		obj.SetSize(int64(len(content)))
		w, err := obj.Writer()
		if err != nil {
			return "", fmt.Errorf("blob writer: %w", err)
		}
		if _, err := w.Write(content); err != nil {
			return "", fmt.Errorf("write blob: %w", err)
		}
		if err := w.Close(); err != nil {
			return "", fmt.Errorf("close blob: %w", err)
		}
		blobHash, err := storer.SetEncodedObject(obj)
		if err != nil {
			return "", fmt.Errorf("store blob: %w", err)
		}
		entries = append(entries, object.TreeEntry{
			Name: path,
			Mode: 0o100644,
			Hash: blobHash,
		})
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	tree := &object.Tree{Entries: entries, Hash: plumbing.ZeroHash}
	treeObj := storer.NewEncodedObject()
	if err := tree.Encode(treeObj); err != nil {
		return "", fmt.Errorf("encode tree: %w", err)
	}
	treeHash, err := storer.SetEncodedObject(treeObj)
	if err != nil {
		return "", fmt.Errorf("store tree: %w", err)
	}

	var parents []plumbing.Hash
	if headRef, err := r.repo.Head(); err == nil {
		parents = append(parents, headRef.Hash())
	}

	commit := &object.Commit{
		Hash:         plumbing.ZeroHash,
		Author:       object.Signature{Name: "test", Email: "test@test.com"},
		Committer:    object.Signature{Name: "test", Email: "test@test.com"},
		MergeTag:     "",
		Signature:    "",
		Message:      message,
		TreeHash:     treeHash,
		ParentHashes: parents,
		Encoding:     "",
		ExtraHeaders: nil,
	}

	commitObj := storer.NewEncodedObject()
	if err := commit.Encode(commitObj); err != nil {
		return "", fmt.Errorf("encode commit: %w", err)
	}
	commitHash, err := storer.SetEncodedObject(commitObj)
	if err != nil {
		return "", fmt.Errorf("store commit: %w", err)
	}

	if err := storer.SetReference(plumbing.NewHashReference(plumbing.Master, commitHash)); err != nil {
		return "", fmt.Errorf("set master ref: %w", err)
	}
	if err := storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.Master)); err != nil {
		return "", fmt.Errorf("set HEAD symref: %w", err)
	}

	return commitHash.String(), nil
}

func (r *testGitRepo) ReadBlob(ref string, path string) ([]byte, error) {
	hash, err := r.repo.ResolveRevision(plumbing.Revision(ref))
	if err != nil {
		return nil, fmt.Errorf("resolve ref: %w", err)
	}
	commit, err := r.repo.CommitObject(*hash)
	if err != nil {
		return nil, fmt.Errorf("get commit: %w", err)
	}
	file, err := commit.File(path)
	if err != nil {
		return nil, fmt.Errorf("get file %s: %w", path, err)
	}
	reader, err := file.Reader()
	if err != nil {
		return nil, fmt.Errorf("open reader: %w", err)
	}
	defer func() { _ = reader.Close() }()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read blob: %w", err)
	}
	return data, nil
}

func (r *testGitRepo) ReadFiles(ref string) (map[string][]byte, error) {
	hash, err := r.repo.ResolveRevision(plumbing.Revision(ref))
	if err != nil {
		return nil, fmt.Errorf("resolve ref: %w", err)
	}
	commit, err := r.repo.CommitObject(*hash)
	if err != nil {
		return nil, fmt.Errorf("get commit: %w", err)
	}
	tree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("get tree: %w", err)
	}
	files := make(map[string][]byte)
	for _, entry := range tree.Entries {
		if !entry.Mode.IsFile() {
			continue
		}
		blob, err := r.ReadBlob(ref, entry.Name)
		if err != nil {
			continue
		}
		files[entry.Name] = blob
	}
	return files, nil
}

func (r *testGitRepo) readAllFiles(ref string) (map[string][]byte, error) {
	return r.ReadFiles(ref)
}
