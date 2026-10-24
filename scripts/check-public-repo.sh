#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
cd "$repo_root"

fail() {
  printf 'public-repository check failed: %s\n' "$1" >&2
  exit 1
}

if git grep -n 'github.com/mcpgate/mcpgate' -- ':!scripts/check-public-repo.sh'; then
  fail 'legacy module path remains in tracked files'
fi

[ -f docs/design/universal-mcp-gateway.md ] || fail 'public design specification is missing'
[ ! -e universal-mcp-gateway-implementation.md ] || fail 'design specification remains at the repository root'
[ ! -e docs/superpowers ] || fail 'internal execution plans remain in the public tree'
[ ! -e artifacts/release-report.json ] || fail 'stale release report remains in the public tree'

for required_file in \
  LICENSE \
  NOTICE \
  CONTRIBUTING.md \
  CODE_OF_CONDUCT.md \
  SECURITY.md \
  .github/CODEOWNERS \
  .github/ISSUE_TEMPLATE/bug_report.yml \
  .github/ISSUE_TEMPLATE/feature_request.yml \
  .github/ISSUE_TEMPLATE/config.yml \
  .github/pull_request_template.md
do
  [ -f "$required_file" ] || fail "required community file is missing: $required_file"
done

grep -Fq 'Apache-2.0' README.md || fail 'README does not identify the project license'
grep -Fq 'github.com/9OctopusDev/mcpgate' README.md || fail 'README does not use the canonical repository path'

printf 'public-repository check passed\n'
