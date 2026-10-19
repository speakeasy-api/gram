"""go_test that runs with its package directory in the runfiles.

Gazelle generates this in place of rules_go's go_test (see the map_kind
directive in the root BUILD.bazel).
"""

load("@rules_go//go:def.bzl", _go_test = "go_test")

def go_test(name, data = [], **kwargs):
    """Defines a go_test that can read every file in its own package.

    go test runs a package's tests from its source directory, so tests open
    fixtures, golden files and even the package's own sources by relative path.
    Shipping the package directory with the test keeps that working without
    listing each file. Files in other packages still go in data.

    Args:
      name: target name.
      data: extra runtime files, typically from other packages.
      **kwargs: passed to rules_go's go_test.
    """
    own = native.glob(["**"], exclude = ["BUILD.bazel"], allow_empty = True)
    _go_test(
        name = name,
        # Gazelle lists testdata/** in data too; drop what own already covers.
        data = own + [d for d in data if d not in own],
        **kwargs
    )
