package main

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseShard(t *testing.T) {
	t.Parallel()

	index, total, err := parseShard("1/4")
	require.NoError(t, err)
	require.Equal(t, 1, index)
	require.Equal(t, 4, total)

	index, total, err = parseShard("4/4")
	require.NoError(t, err)
	require.Equal(t, 4, index)
	require.Equal(t, 4, total)
}

func TestParseShard_Invalid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		spec    string
		wantErr string
	}{
		{spec: "", wantErr: "missing -i"},
		{spec: "1", wantErr: "not in <index>/<total> form"},
		{spec: "one/4", wantErr: `shard index "one" is not a number`},
		{spec: "1/four", wantErr: `shard total "four" is not a number`},
		{spec: "1/0", wantErr: "shard total 0 must be at least 1"},
		{spec: "0/4", wantErr: "shard index 0 must be between 1 and 4"},
		{spec: "5/4", wantErr: "shard index 5 must be between 1 and 4"},
		{spec: "-1/4", wantErr: "shard index -1 must be between 1 and 4"},
	}

	for _, c := range cases {
		_, _, err := parseShard(c.spec)
		require.ErrorContains(t, err, c.wantErr, "spec %q", c.spec)
	}
}

func TestExpandPattern(t *testing.T) {
	t.Parallel()

	require.Equal(t, "./server/...", expandPattern("./server"))
	require.Equal(t, "./server/...", expandPattern("./server/"))
	require.Equal(t, "./...", expandPattern("."))
	require.Equal(t, "./server/...", expandPattern("./server/..."))
	require.Equal(t, "./internal/oops/...", expandPattern("./internal/oops/..."))
}

func TestAssign_CoversEveryPackageExactlyOnce(t *testing.T) {
	t.Parallel()

	const total = 4
	pkgs := testPackages(97)

	var got []string
	for index := 1; index <= total; index++ {
		for _, p := range assign(pkgs, index, total) {
			got = append(got, p.ImportPath)
		}
	}

	var want []string
	for _, p := range pkgs {
		want = append(want, p.ImportPath)
	}

	slices.Sort(got)
	slices.Sort(want)
	require.Equal(t, want, got, "shards must partition the package list")
}

func TestAssign_SingleShardTakesEverything(t *testing.T) {
	t.Parallel()

	pkgs := testPackages(10)
	require.Len(t, assign(pkgs, 1, 1), len(pkgs))
}

func TestAssign_MoreShardsThanPackages(t *testing.T) {
	t.Parallel()

	pkgs := testPackages(2)

	require.Len(t, assign(pkgs, 1, 4), 1)
	require.Len(t, assign(pkgs, 2, 4), 1)
	require.Empty(t, assign(pkgs, 3, 4))
	require.Empty(t, assign(pkgs, 4, 4))
}

func TestAssign_ShardCountFarBeyondPackageCount(t *testing.T) {
	t.Parallel()

	pkgs := testPackages(3)

	// Tracking a load per requested shard would allocate 8GB here.
	require.Len(t, assign(pkgs, 1, 1_000_000_000), 1)
	require.Empty(t, assign(pkgs, 4, 1_000_000_000))
}

func TestAssign_BalancesWeight(t *testing.T) {
	t.Parallel()

	const total = 4
	pkgs := testPackages(97)

	var heaviest, lightest int64
	for index := 1; index <= total; index++ {
		var load int64
		for _, p := range assign(pkgs, index, total) {
			load += p.weight
		}

		if index == 1 || load > heaviest {
			heaviest = load
		}
		if index == 1 || load < lightest {
			lightest = load
		}
	}

	// Packing largest first keeps shards within one package's weight of each
	// other; the widest gap here is a fraction of that.
	require.Less(t, heaviest-lightest, int64(1000), "shard loads drifted too far apart")
}

func TestAssign_IgnoresInputOrder(t *testing.T) {
	t.Parallel()

	pkgs := testPackages(50)
	reversed := slices.Clone(pkgs)
	slices.Reverse(reversed)

	for index := 1; index <= 3; index++ {
		require.Equal(t, assign(pkgs, index, 3), assign(reversed, index, 3),
			"assignment must not depend on the order go list returned packages in")
	}
}

// testPackages builds n packages whose weights vary over a wide range so that
// balancing has something to do.
func testPackages(n int) []pkg {
	pkgs := make([]pkg, 0, n)
	for i := range n {
		pkgs = append(pkgs, pkg{
			ImportPath:   fmt.Sprintf("example.com/mod/pkg%02d", i),
			Dir:          fmt.Sprintf("/mod/pkg%02d", i),
			TestGoFiles:  []string{"main_test.go"},
			XTestGoFiles: nil,
			weight:       int64(1 + (i*7919)%5000),
		})
	}

	return pkgs
}
