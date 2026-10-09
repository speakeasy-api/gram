#!/usr/bin/env bash

#MISE description="Block new 'Gram' text in added lines; the product is called Speakeasy"

#USAGE flag "--base <ref>" default="origin/main" help="Check lines added since the merge base with this ref"
#USAGE flag "--staged" help="Check staged lines only (used by the pre-commit hook)"

# This is a ratchet, not a repo-wide ban: thousands of legacy "Gram" strings
# remain (headers, generated code, the SDK class) and are renamed separately.
# Only lines a change adds are checked, so the count can only go down.
#
# Allowed without comment: HTTP headers (Gram-Key, X-Gram-*), GRAM_* env vars,
# code uses of the SDK/Functions `Gram` class, and import lines. For any other
# intentional use, put "brand-ok" on the line with a short reason.

set -euo pipefail

if [ "${usage_staged:-false}" = "true" ]; then
  range=(--cached)
else
  range=("$(git merge-base "${usage_base:-origin/main}" HEAD)")
fi

git diff "${range[@]}" -U0 --no-color --no-ext-diff -- . \
  ':(exclude)server/migrations/**' \
  ':(exclude)server/gen/**' \
  ':(exclude)**/gen/**' \
  ':(exclude).speakeasy/**' \
  ':(exclude)client/sdk/**' \
  ':(exclude)**/CHANGELOG.md' \
  ':(exclude)*.lock' \
  ':(exclude)pnpm-lock.yaml' \
  ':(exclude)atlas.sum' \
  ':(exclude).mise-tasks/lint/brand.sh' |
  perl -ne '
    if (/^\+\+\+ b\/(.*)/) { $file = $1; next }
    if (/^@@ -\S+ \+(\d+)/) { $line = $1; next }
    next unless /^\+/;
    my $text = substr($_, 1);
    my $n = $line++;
    next if $text =~ /brand-ok:\s*\S/;
    (my $s = $text) =~ s{
        \bX-Gram-[\w*-]*        # HTTP headers, including the X-Gram-* family
      | \bGram-[A-Z][\w-]*      # HTTP headers (Gram-Key, Gram-Project, ...)
      | Speakeasy-Gram/         # device-management user agent
    }{}gx;
    # Code uses of the `Gram` class are allowed, but only on code lines of code
    # files, so prose such as "the old product (Gram)" in docs and comments
    # cannot slip through these patterns.
    my $code = $file =~ /\.(?:[cm]?[jt]sx?|go|py)$/ && $text !~ m{^\s*(?://|/\*|\*|\#|--)};
    next if $text =~ /^[\s\-+]*(import\b|\}\s*from\b)/;
    if ($code) {
      $s =~ s{
          ^\s*(?:type\s+)?Gram(?:\s+as\s+\w+)?,?\s*$   # wrapped import specifier
        | \b(?:new|class|extends|typeof|const|let|type)\s+Gram\b
        | \.Gram\b                # window.Gram, sdk.Gram
        | (?:[(<\[*]|,\s*)Gram(?=[)>\],;\[]|\s*$)   # argument or type parameter
        | ^\s*Gram\s+(?:struct\s*\{|\*?[\w.\[\]]+\s*(?:`.*)?)$   # Go struct field
        | \bGram(?=[(<]|\.\w|\[\])   # Gram(...), Gram.tool, Gram<T>, Gram[]
        | :\s*Gram\b(?=\s*(?:[;,)=|>\[]|$))   # type annotations
        | ["\x27`]Gram["\x27`]    # the literal needle in "never says Gram" tests
      }{}gx;
    }
    if ($s =~ /\bGram\b/) {
      chomp $text;
      print "$file:$n: $text\n";
      $bad++;
    }
    END {
      exit 0 unless $bad;
      print STDERR "\n$bad added line(s) say \"Gram\". The product is called Speakeasy.\n";
      print STDERR "Use \"Speakeasy\" in user-facing text, error messages, comments, and docs.\n";
      print STDERR "If the line must keep \"Gram\" (e.g. contrasting with the legacy platform),\n";
      print STDERR "add \"brand-ok: <reason>\" on the same line.\n";
      exit 1;
    }
  '
