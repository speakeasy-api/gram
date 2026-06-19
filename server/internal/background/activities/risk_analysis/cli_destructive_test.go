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
		{name: "destructive.shell.rm_rf", input: "rm -rf /tmp/work", fullName: "destructive.shell.rm_rf"},
		{name: "destructive.shell.rm_rf-glob", input: "sudo rm -rf *", fullName: "destructive.shell.rm_rf"}, // rm_rf precedes sudo in declaration order
		{name: "destructive.shell.dd", input: "dd if=/dev/zero of=/dev/sda bs=1M", fullName: "destructive.shell.dd"},
		{name: "destructive.shell.mkfs", input: "mkfs.ext4 /dev/sdb1", fullName: "destructive.shell.mkfs"},
		{name: "destructive.shell.fork_bomb", input: ":(){ :|: & };:", fullName: "destructive.shell.fork_bomb"},
		{name: "destructive.shell.chmod_recursive", input: "chmod -R 777 /etc", fullName: "destructive.shell.chmod_recursive"},
		{name: "destructive.shell.chown_recursive", input: "chown --recursive root:root /home", fullName: "destructive.shell.chown_recursive"},
		{name: "destructive.shell.sudo", input: "sudo cat /etc/shadow", fullName: "destructive.shell.sudo"},
		{name: "destructive.git.push_force", input: "git push --force origin main", fullName: "destructive.git.push_force"},
		{name: "destructive.git.push_force-shorthand", input: "git push -f origin main", fullName: "destructive.git.push_force"},
		{name: "destructive.git.push_force-with-lease", input: "git push --force-with-lease origin main", fullName: "destructive.git.push_force"},
		{name: "destructive.git.reset_hard", input: "git reset --hard HEAD~3", fullName: "destructive.git.reset_hard"},
		{name: "destructive.git.clean_force", input: "git clean -fd", fullName: "destructive.git.clean_force"},
		{name: "destructive.git.branch_delete_force", input: "git branch -D feature/x", fullName: "destructive.git.branch_delete_force"},
		{name: "destructive.database.drop-table", input: "DROP TABLE users", fullName: "destructive.database.drop"},
		{name: "destructive.database.drop-database", input: "DROP DATABASE prod", fullName: "destructive.database.drop"},
		{name: "destructive.database.truncate", input: "TRUNCATE accounts", fullName: "destructive.database.truncate"},
		{name: "destructive.database.dropdb", input: "dropdb prod", fullName: "destructive.database.dropdb"},
		{name: "destructive.cloud.aws_ec2_terminate", input: "aws ec2 terminate-instances --instance-ids i-1234", fullName: "destructive.cloud.aws_ec2_terminate"},
		{name: "destructive.cloud.aws_s3_rb", input: "aws s3 rb s3://my-bucket --force", fullName: "destructive.cloud.aws_s3_rb"},
		{name: "destructive.cloud.gcloud_projects_delete", input: "gcloud projects delete my-project", fullName: "destructive.cloud.gcloud_projects_delete"},
		{name: "destructive.cloud.kubectl_delete_namespace", input: "kubectl delete ns production", fullName: "destructive.cloud.kubectl_delete_namespace"},
		{name: "destructive.cloud.kubectl_delete-deployment", input: "kubectl delete deployment api", fullName: "destructive.cloud.kubectl_delete_workload"},
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
		assert.Equal(t, "destructive.database.delete_without_where", unguarded.FullName())
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
			assert.Equal(t, "destructive.shell.rm_rf", matched.FullName())
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
			assert.Equal(t, "destructive.database.drop", matched.FullName())
		}
	})

	t.Run("slice element", func(t *testing.T) {
		t.Parallel()
		input := map[string]any{
			"command": []any{"git", "push", "--force"},
		}
		// Each slice element is scanned individually — "git push --force" only
		// matches when assembled. So this doesn't trip destructive.git.push_force, which
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
