package hooks

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScanForDestructive_positiveByCategory(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		input       any
		wantCat     string
		wantPattern string
	}{
		// Shell.
		{name: "rm -rf", input: map[string]any{"command": "rm -rf /tmp/foo"}, wantCat: "shell", wantPattern: "rm-rf"},
		{name: "rm -rf star", input: map[string]any{"command": "rm -rf *"}, wantCat: "shell", wantPattern: "rm-rf"},
		{name: "rm -fr alt order", input: map[string]any{"command": "rm -fr /var/log"}, wantCat: "shell", wantPattern: "rm-rf"},
		{name: "dd write", input: map[string]any{"command": "dd if=/dev/zero of=/dev/sda bs=4M"}, wantCat: "shell", wantPattern: "dd"},
		{name: "mkfs ext4", input: map[string]any{"command": "mkfs.ext4 /dev/sdb1"}, wantCat: "shell", wantPattern: "mkfs"},
		{name: "fork bomb", input: map[string]any{"command": ":(){ :|:& };:"}, wantCat: "shell", wantPattern: "fork-bomb"},
		{name: "chmod -R", input: map[string]any{"command": "chmod -R 777 /etc"}, wantCat: "shell", wantPattern: "chmod-recursive"},
		{name: "chown -R", input: map[string]any{"command": "chown -R nobody:nobody /var"}, wantCat: "shell", wantPattern: "chown-recursive"},
		{name: "sudo", input: map[string]any{"command": "sudo something dangerous"}, wantCat: "shell", wantPattern: "sudo"},

		// Git.
		{name: "git push --force", input: map[string]any{"command": "git push origin main --force"}, wantCat: "git", wantPattern: "push-force"},
		{name: "git push -f", input: map[string]any{"command": "git push -f origin main"}, wantCat: "git", wantPattern: "push-force"},
		{name: "git reset --hard", input: map[string]any{"command": "git reset --hard HEAD~3"}, wantCat: "git", wantPattern: "reset-hard"},
		{name: "git clean -fd", input: map[string]any{"command": "git clean -fd"}, wantCat: "git", wantPattern: "clean-force"},
		{name: "branch -D bare", input: map[string]any{"command": "branch -D feature/foo"}, wantCat: "git", wantPattern: "branch-delete-force"},
		{name: "git branch -D", input: map[string]any{"command": "git branch -D feature/foo"}, wantCat: "git", wantPattern: "branch-delete-force"},

		// Database.
		{name: "DROP TABLE", input: map[string]any{"query": "DROP TABLE users"}, wantCat: "database", wantPattern: "drop"},
		{name: "drop database", input: map[string]any{"query": "drop database production"}, wantCat: "database", wantPattern: "drop"},
		{name: "TRUNCATE", input: map[string]any{"query": "TRUNCATE orders"}, wantCat: "database", wantPattern: "truncate"},
		{name: "delete without where", input: map[string]any{"query": "DELETE FROM users"}, wantCat: "database", wantPattern: "delete-without-where"},
		{name: "dropdb cli", input: map[string]any{"command": "dropdb production"}, wantCat: "database", wantPattern: "dropdb"},

		// Cloud.
		{name: "aws ec2 terminate-instances", input: map[string]any{"command": "aws ec2 terminate-instances --instance-ids i-abc"}, wantCat: "cloud", wantPattern: "aws-ec2-terminate"},
		{name: "aws s3 rb", input: map[string]any{"command": "aws s3 rb s3://my-bucket --force"}, wantCat: "cloud", wantPattern: "aws-s3-rb"},
		{name: "gcloud projects delete", input: map[string]any{"command": "gcloud projects delete my-prod"}, wantCat: "cloud", wantPattern: "gcloud-projects-delete"},
		{name: "kubectl delete ns", input: map[string]any{"command": "kubectl delete ns prod"}, wantCat: "cloud", wantPattern: "kubectl-delete-namespace"},
		{name: "kubectl delete namespace", input: map[string]any{"command": "kubectl delete namespace prod"}, wantCat: "cloud", wantPattern: "kubectl-delete-namespace"},
		{name: "kubectl delete deployment", input: map[string]any{"command": "kubectl delete deployment api"}, wantCat: "cloud", wantPattern: "kubectl-delete-workload"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			matched, ok := scanForDestructive(tc.input)
			require.True(t, ok, "expected match for %s", tc.name)
			require.Equal(t, tc.wantCat, matched.Category)
			require.Equal(t, tc.wantPattern, matched.Name)
		})
	}
}

func TestScanForDestructive_negative(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input any
	}{
		{name: "empty map", input: map[string]any{}},
		{name: "nil", input: nil},
		{name: "ls", input: map[string]any{"command": "ls -la"}},
		{name: "git status", input: map[string]any{"command": "git status"}},
		{name: "git push (no force)", input: map[string]any{"command": "git push origin main"}},
		{name: "select", input: map[string]any{"query": "SELECT * FROM users"}},
		{name: "delete with where", input: map[string]any{"query": "DELETE FROM users WHERE id = 1"}},
		{name: "rm without -rf", input: map[string]any{"command": "rm /tmp/foo"}},
		{name: "kubectl get pods", input: map[string]any{"command": "kubectl get pods"}},
		{name: "aws ec2 describe", input: map[string]any{"command": "aws ec2 describe-instances"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, ok := scanForDestructive(tc.input)
			require.False(t, ok, "did not expect match for %s", tc.name)
		})
	}
}

func TestScanForDestructive_findsInNestedStructures(t *testing.T) {
	t.Parallel()

	// Pattern in a nested map value.
	nested := map[string]any{
		"meta": map[string]any{
			"command": "rm -rf /tmp/foo",
		},
	}
	matched, ok := scanForDestructive(nested)
	require.True(t, ok)
	require.Equal(t, "shell", matched.Category)

	// Pattern in a slice element.
	slice := map[string]any{
		"args": []any{"--force", "git push --force origin main"},
	}
	matched, ok = scanForDestructive(slice)
	require.True(t, ok)
	require.Equal(t, "git", matched.Category)

	// Pattern in a deeply nested combination.
	deep := map[string]any{
		"args": []any{
			map[string]any{
				"sql": "DROP TABLE users",
			},
		},
	}
	matched, ok = scanForDestructive(deep)
	require.True(t, ok)
	require.Equal(t, "database", matched.Category)
}

func TestScanForDestructive_ignoresNonStringValues(t *testing.T) {
	t.Parallel()

	// Numbers and bools should not match anything.
	input := map[string]any{
		"timeout": 30,
		"force":   true,
		"opt":     nil,
	}
	_, ok := scanForDestructive(input)
	require.False(t, ok)
}

func TestFlattenToolInputStrings_collectsValues(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"command": "ls",
		"meta": map[string]any{
			"label": "first",
		},
		"args":    []any{"a", "b", 7, true},
		"empty":   "",
		"trailer": "trailing",
	}

	got := flattenToolInputStrings(input)
	require.ElementsMatch(t, []string{"ls", "first", "a", "b", "trailing"}, got)
}

func TestFlattenToolInputStrings_handlesScalarRoot(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{"rm -rf /"}, flattenToolInputStrings("rm -rf /"))
	require.Empty(t, flattenToolInputStrings(nil))
	require.Empty(t, flattenToolInputStrings(42))
}

func TestDestructivePattern_FullName(t *testing.T) {
	t.Parallel()

	p := destructivePattern{Category: "shell", Name: "rm-rf"}
	require.Equal(t, "shell/rm-rf", p.FullName())

	require.Empty(t, destructivePattern{}.FullName())
}
