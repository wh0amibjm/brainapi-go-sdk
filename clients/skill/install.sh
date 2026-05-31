#!/usr/bin/env bash
# install.sh — one-command setup for the `brainapi` Agent Skill.
#
# Installs the skill into a Claude Code skills directory, makes its helper
# scripts executable, and runs a quick environment doctor (binary / jq /
# credentials) with copy-pasteable fixes for anything missing.
#
# Works two ways:
#   • From a clone:   bash clients/skill/install.sh            (copies local files)
#   • Public one-liner once the repo is public:
#       curl -fsSL https://raw.githubusercontent.com/wh0amibjm/brainapi-go-sdk/main/clients/skill/install.sh | bash
#     (the script then fetches the skill files from GitHub raw)
#
# Options:
#   --global            install to ~/.claude/skills/brainapi          (default)
#   --project [DIR]     install to <DIR>/.claude/skills/brainapi      (DIR defaults to cwd)
#   --ref <git-ref>     branch/tag to fetch from in remote mode       (default: main)
#   -h, --help
set -euo pipefail

REPO="wh0amibjm/brainapi-go-sdk"
REF="main"
SCOPE="global"
PROJECT_DIR=""
SKILL_NAME="brainapi"
# Files that make up the skill (relative to clients/skill/). Update if the
# skill gains files — only matters for the remote (curl) install path.
SKILL_FILES=("brainapi/SKILL.md" "brainapi/scripts/safe-submit.sh")

c_ok()   { printf '  \033[32m✓\033[0m %s\n' "$*"; }
c_warn() { printf '  \033[33m!\033[0m %s\n' "$*"; }
c_info() { printf '\033[36m==>\033[0m %s\n' "$*"; }
die()    { printf '\033[31minstall: %s\033[0m\n' "$*" >&2; exit 2; }

while [ $# -gt 0 ]; do
  case "$1" in
    --global)  SCOPE="global"; shift ;;
    --project) SCOPE="project"; if [ "${2:-}" ] && [ "${2#-}" = "$2" ]; then PROJECT_DIR="$2"; shift 2; else PROJECT_DIR="$PWD"; shift; fi ;;
    --ref)     REF="${2:?--ref needs a value}"; shift 2 ;;
    -h|--help) sed -n '2,28p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) die "unknown option: $1 (try --help)" ;;
  esac
done

# Resolve the skills directory and the final target.
if [ "$SCOPE" = "project" ]; then
  base="${PROJECT_DIR:-$PWD}"
  [ -d "$base" ] || die "project dir not found: $base"
  SKILLS_DIR="$base/.claude/skills"
else
  SKILLS_DIR="${HOME:?HOME unset}/.claude/skills"
fi
TARGET="$SKILLS_DIR/$SKILL_NAME"
# Guard the rm: TARGET must end in /skills/brainapi.
case "$TARGET" in */skills/"$SKILL_NAME") ;; *) die "refusing unsafe target: $TARGET" ;; esac

# Locate the skill source: local clone if present, else fetch from GitHub raw.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd || echo "")"
SRC=""
if [ -n "$SCRIPT_DIR" ] && [ -f "$SCRIPT_DIR/$SKILL_NAME/SKILL.md" ]; then
  SRC="$SCRIPT_DIR"
  c_info "Source: local clone ($SRC)"
else
  c_info "Source: GitHub raw ($REPO@$REF)"
  command -v curl >/dev/null 2>&1 || die "curl required for remote install (or run from a clone)"
  SRC="$(mktemp -d)"
  trap 'rm -rf "$SRC"' EXIT
  base_url="https://raw.githubusercontent.com/$REPO/$REF/clients/skill"
  for rel in "${SKILL_FILES[@]}"; do
    mkdir -p "$SRC/$(dirname "$rel")"
    curl -fsSL "$base_url/$rel" -o "$SRC/$rel" || die "fetch failed: $base_url/$rel"
  done
fi

# Install (idempotent: replace the skill dir wholesale).
c_info "Installing skill → $TARGET"
mkdir -p "$SKILLS_DIR"
rm -rf "$TARGET"
mkdir -p "$TARGET"
cp -R "$SRC/$SKILL_NAME/." "$TARGET/"
chmod +x "$TARGET"/scripts/*.sh 2>/dev/null || true
c_ok "skill files installed"

# Doctor — surface prerequisites; don't fail the install over them.
c_info "Checking environment"
BIN="${BRAINAPI_BIN:-brainapi}"
if command -v "$BIN" >/dev/null 2>&1; then
  c_ok "brainapi found: $("$BIN" version 2>/dev/null | tr -d '\n' | sed 's/.*"version": *"\([^"]*\)".*/\1/' || echo present)"
else
  c_warn "brainapi not on PATH. Get it with:"
  printf '      go install github.com/%s/cmd/brainapi@latest\n' "$REPO"
  printf '      (or download a release binary, then export BRAINAPI_BIN=/path/to/brainapi)\n'
fi
command -v jq >/dev/null 2>&1 && c_ok "jq found" || c_warn "jq not found (needed by safe-submit.sh) — install via your package manager."
if [ -n "${BRAINAPI_USER:-}" ] && [ -n "${BRAINAPI_PASS:-}" ]; then
  c_ok "BRAINAPI_USER / BRAINAPI_PASS set"
else
  c_warn "BRAINAPI_USER / BRAINAPI_PASS not set — export them (or pass --user/--pass) before live calls."
fi

echo
c_info "Done. Open a new Claude Code session in this scope and ask about a BRAIN alpha,"
c_info "or invoke it explicitly with /$SKILL_NAME."
