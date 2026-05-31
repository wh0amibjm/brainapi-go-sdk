# brainapi skill (Claude Code / Agent Skill)

An [Agent Skill](https://docs.claude.com/en/docs/claude-code/skills) that teaches
a Claude-family coding agent how to drive the `brainapi` CLI against the
WorldQuant BRAIN API — *with the judgment and safety rails that the raw command
surface doesn't carry* (self-correlation gating before submit, exit-code
handling, and an explicit confirm-before-mutating model).

It is the AI-era counterpart to manual integration (the Go library /
[`clients/typescript`](../typescript) subprocess wrapper): instead of you writing
glue code, the agent calls the CLI directly under this skill's guidance.

## What it needs

- The `brainapi` binary on `PATH` (or `BRAINAPI_BIN` pointing at it). Build with
  `go build -o brainapi ./cmd/brainapi`, or grab a release binary.
- `jq` (used by the bundled `safe-submit.sh` helper).
- `BRAINAPI_USER` / `BRAINAPI_PASS` in the environment (or pass `--user`/`--pass`).

## Install — one command

**macOS / Linux / WSL / Git-Bash** (from a clone):
```bash
make install-skill                  # → ~/.claude/skills/brainapi
# or, directly:
bash clients/skill/install.sh             # global  (~/.claude/skills)
bash clients/skill/install.sh --project   # this project's .claude/skills
```

**Windows** (PowerShell — Windows PowerShell 5.1 or PowerShell 7, from a clone):
```powershell
powershell -ExecutionPolicy Bypass -File clients\skill\install.ps1
# or for a project: ... install.ps1 -Project          (add -Dir <path> to target another dir)
```

Without a clone (once the repo is public):
```bash
# macOS / Linux:
curl -fsSL https://raw.githubusercontent.com/wh0amibjm/brainapi-go-sdk/main/clients/skill/install.sh | bash
```
```powershell
# Windows:
irm https://raw.githubusercontent.com/wh0amibjm/brainapi-go-sdk/main/clients/skill/install.ps1 | iex
```

Both installers do the same thing — copy the skill, make the helper executable
(no-op on Windows), and run a quick doctor (brainapi binary / `jq` /
`BRAINAPI_USER`+`PASS`) printing a copy-pasteable fix for anything missing.
They're idempotent — re-run to update.

> **Windows note:** the bundled `safe-submit.sh` is a bash script; it runs under
> the Git Bash / WSL environment Claude Code uses for its shell on Windows, so
> make sure `jq` is available there (`winget install jqlang.jq`, scoop, or choco).

The skill auto-triggers when a task mentions WorldQuant BRAIN alphas /
simulations / the brainapi CLI; you can also invoke it explicitly with
`/brainapi`.

<details><summary>Manual install (no script)</summary>

```bash
cp -r clients/skill/brainapi ~/.claude/skills/brainapi
chmod +x ~/.claude/skills/brainapi/scripts/*.sh
```
</details>

## Layout

```
brainapi/
├── SKILL.md                 # instructions: discovery, output contract, safety model, workflows
└── scripts/
    └── safe-submit.sh       # gated submit: corr-gate -> budget context -> dry-run -> --confirm
```

## Staying in sync with the SDK

The skill does **not** hardcode the command list. It instructs the agent to run
`brainapi describe` for the authoritative, version-pinned contract (commands,
exit codes, error kinds, protocol gotchas), so it tracks the SDK automatically.
When you change the CLI surface, no skill edit is needed unless a *workflow* or
*safety* judgment changes.

## Safety model (summary)

Read-only commands run freely. Mutating/scarce ones — `alphas submit`,
`simulations create`, `register`, `email`/`password` — require an explicit
in-turn confirmation and are never looped. `alphas submit` always passes the
`alphas corr` gate (`max < 0.7`) first. See `brainapi/SKILL.md` for the full lists.
