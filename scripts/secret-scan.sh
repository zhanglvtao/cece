#!/usr/bin/env bash
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

fail=0

is_text_file() {
  local file="$1"
  [[ -f "$file" ]] || return 1
  grep -Iq . "$file"
}

scan_file() {
  local file="$1"

  awk -v file="$file" '
function allowed(line, lower) {
  if (file ~ /(^README\.md$|^docs\/|\.example\.)/) {
    if (lower ~ /(xxxxx|example|placeholder|dummy|your-|my-token|api\.anthropic\.com|mcp\.example\.com|code_user_jwt|cece_codebase_auth_helper|cece_record_llm|anthropic_api_key|anthropic_base_url|anthropic_model)/) {
      return 1
    }
  }
  return 0
}

function hit(rule, line, lower) {
  if (allowed(line, lower)) {
    return
  }
  printf "%s:%d: %s\n", file, FNR, rule
  found = 1
}

{
  line = $0
  lower = tolower(line)

  if (line ~ /-----BEGIN[[:space:]]+([A-Z0-9]+[[:space:]]+)*PRIVATE[[:space:]]+KEY-----/) hit("private key block", line, lower)
  if (line ~ /AKIA[0-9A-Z]{16}/) hit("aws access key", line, lower)
  if (line ~ /ASIA[0-9A-Z]{16}/) hit("aws temporary access key", line, lower)
  if (line ~ /ghp_[A-Za-z0-9_]{36,}/) hit("github personal access token", line, lower)
  if (line ~ /github_pat_[A-Za-z0-9_]{40,}/) hit("github fine-grained token", line, lower)
  if (line ~ /sk-ant-[A-Za-z0-9_-]{20,}/) hit("anthropic api key", line, lower)
  if (line ~ /sk-[A-Za-z0-9]{32,}/) hit("openai-style api key", line, lower)
  if (line ~ /xox[baprs]-[A-Za-z0-9-]{20,}/) hit("slack token", line, lower)
  if (line ~ /eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}/) hit("jwt", line, lower)
  if (lower ~ /(api[_-]?key|token|secret|password)[[:space:]]*[:=][[:space:]]*["\047]?[A-Za-z0-9_\.\/+=-]{20,}/) hit("hardcoded credential field", line, lower)
}

END {
  exit found ? 1 : 0
}
' "$file"
}

while IFS= read -r -d '' file; do
  is_text_file "$file" || continue
  if ! scan_file "$file"; then
    fail=1
  fi
done < <(git ls-files -z)

if [[ "$fail" -ne 0 ]]; then
  echo "secret scan failed"
  exit 1
fi

echo "secret scan passed"
