package risk_analysis

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestMatchCLIDestructiveString_CuratedPatterns exercises one representative
// example per curated pattern. Each case is named after the FullName the
// matcher is expected to return.
func TestMatchCLIDestructiveString_CuratedPatterns(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		input    string
		fullName string
	}{
		{name: "shell/rm-rf", input: "rm -rf /tmp/work", fullName: "shell/rm-rf"},
		{name: "shell/rm-rf-glob", input: "sudo rm -rf *", fullName: "shell/rm-rf"}, // rm-rf precedes sudo in declaration order
		{name: "shell/dd", input: "dd if=/dev/zero of=/dev/sda bs=1M", fullName: "shell/dd"},
		{name: "shell/mkfs", input: "mkfs.ext4 /dev/sdb1", fullName: "shell/mkfs"},
		{name: "shell/fork-bomb", input: ":(){ :|: & };:", fullName: "shell/fork-bomb"},
		{name: "shell/chmod-recursive", input: "chmod -R 777 /etc", fullName: "shell/chmod-recursive"},
		{name: "shell/chown-recursive", input: "chown --recursive root:root /home", fullName: "shell/chown-recursive"},
		{name: "shell/sudo", input: "sudo cat /etc/shadow", fullName: "shell/sudo"},
		{name: "git/push-force", input: "git push --force origin main", fullName: "git/push-force"},
		{name: "git/push-force-shorthand", input: "git push -f origin main", fullName: "git/push-force"},
		{name: "git/push-force-with-lease", input: "git push --force-with-lease origin main", fullName: "git/push-force"},
		{name: "git/reset-hard", input: "git reset --hard HEAD~3", fullName: "git/reset-hard"},
		{name: "git/clean-force", input: "git clean -fd", fullName: "git/clean-force"},
		{name: "git/branch-delete-force", input: "git branch -D feature/x", fullName: "git/branch-delete-force"},
		{name: "database/drop-table", input: "DROP TABLE users", fullName: "database/drop"},
		{name: "database/drop-database", input: "DROP DATABASE prod", fullName: "database/drop"},
		{name: "database/truncate", input: "TRUNCATE accounts", fullName: "database/truncate"},
		{name: "database/dropdb", input: "dropdb prod", fullName: "database/dropdb"},
		{name: "cloud/aws-ec2-terminate", input: "aws ec2 terminate-instances --instance-ids i-1234", fullName: "cloud/aws-ec2-terminate"},
		{name: "cloud/aws-s3-rb", input: "aws s3 rb s3://my-bucket --force", fullName: "cloud/aws-s3-rb"},
		{name: "cloud/gcloud-projects-delete", input: "gcloud projects delete my-project", fullName: "cloud/gcloud-projects-delete"},
		{name: "cloud/kubectl-delete-namespace", input: "kubectl delete ns production", fullName: "cloud/kubectl-delete-namespace"},
		{name: "cloud/kubectl-delete-deployment", input: "kubectl delete deployment api", fullName: "cloud/kubectl-delete-workload"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			matched, ok := matchCLIDestructiveString(tc.input)
			if assert.True(t, ok, "expected pattern match for %q", tc.input) {
				assert.Equal(t, tc.fullName, matched.FullName())
			}
		})
	}
}

// TestMatchCLIDestructiveString_DeleteWhereGuard exercises the Guard:
// DELETE FROM ... WHERE is benign, DELETE FROM without WHERE flags.
func TestMatchCLIDestructiveString_DeleteWhereGuard(t *testing.T) {
	t.Parallel()

	guarded, ok := matchCLIDestructiveString("DELETE FROM users WHERE id = 7")
	assert.False(t, ok, "DELETE with WHERE clause must not flag, got %q", guarded.FullName())

	unguarded, ok := matchCLIDestructiveString("DELETE FROM users")
	if assert.True(t, ok, "DELETE without WHERE must flag") {
		assert.Equal(t, "database/delete-without-where", unguarded.FullName())
	}
}

func TestMatchCLIDestructiveString_BenignInputs(t *testing.T) {
	t.Parallel()

	cases := []string{
		"ls -la",
		"cat README.md",
		"git status",
		"git push origin main", // no force flag
		"echo hello world",
		"npm run test",
		"SELECT * FROM users WHERE id = 7",
		"",
		// Bare-`sudo` mentions in prose must not flag — the curated pattern
		// requires at least one argument.
		"run with sudo",
		"sudo",
	}
	for _, in := range cases {
		matched, ok := matchCLIDestructiveString(in)
		assert.False(t, ok, "expected no match for %q, got %q", in, matched.FullName())
	}
}

// TestScanForCLIDestructive_NestedStructures verifies the matcher walks into
// maps and slices to find destructive content nested anywhere in tool args.
func TestScanForCLIDestructive_NestedStructures(t *testing.T) {
	t.Parallel()

	t.Run("map value", func(t *testing.T) {
		t.Parallel()
		input := map[string]any{"command": "rm -rf /tmp/x"}
		matched, ok := scanForCLIDestructive(input)
		if assert.True(t, ok) {
			assert.Equal(t, "shell/rm-rf", matched.FullName())
		}
	})

	t.Run("nested map", func(t *testing.T) {
		t.Parallel()
		input := map[string]any{
			"args": map[string]any{
				"shell": "DROP TABLE users",
			},
		}
		matched, ok := scanForCLIDestructive(input)
		if assert.True(t, ok) {
			assert.Equal(t, "database/drop", matched.FullName())
		}
	})

	t.Run("slice element", func(t *testing.T) {
		t.Parallel()
		input := map[string]any{
			"command": []any{"git", "push", "--force"},
		}
		// Each slice element is scanned individually — "git push --force" only
		// matches when assembled. So this doesn't trip git/push-force, which
		// is the documented behavior (we don't reconstruct shell argv).
		_, ok := scanForCLIDestructive(input)
		assert.False(t, ok, "argv slice elements are scanned individually")
	})

	t.Run("nil input", func(t *testing.T) {
		t.Parallel()
		_, ok := scanForCLIDestructive(nil)
		assert.False(t, ok)
	})

	t.Run("benign nested", func(t *testing.T) {
		t.Parallel()
		input := map[string]any{"command": "ls -la"}
		_, ok := scanForCLIDestructive(input)
		assert.False(t, ok)
	})
}
