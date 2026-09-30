#!/usr/bin/env python3
"""Refuse shell commands that destroy the dev database without a backup.

On 2026-09-30 an agent verifying CI ran `docker compose down -v` on this stack and
deleted the Postgres volume: weeks of polling, and the posting_observations
history that records WHEN each posting was seen, which no re-fetch can restore.
There was no backup.

The destructive Make targets (clean, db-reset, seed-reset) now take one first,
so this blocks only the raw commands that bypass them. An explicitly named
project other than the default is allowed — `docker compose -p jobtrack-verify`
is how a clean slate is reproduced without touching real data.

Only the COMMAND POSITION is judged. The first version matched words anywhere and
blocked its own author's commit, whose message described the incident: a guard
that fires on `git commit`, `echo` or `grep` gets switched off, and then guards
nothing. Heredoc bodies are dropped for the same reason — they are data.

Registered as a PreToolUse hook in .claude/settings.json. Exit 2 blocks the call
and shows stderr to the agent. `--self-test` runs the cases at the bottom.
"""

from __future__ import annotations

import json
import re
import shlex
import sys

DEFAULT_PROJECT = "jobtrack"

# Split a command line into the simple commands it chains. Coarse on purpose:
# a false block costs one rephrased command, a false allow costs the database.
SEPARATORS = re.compile(r"&&|\|\||;|\||\n")
HEREDOC_START = re.compile(r"<<-?\s*['\"]?(\w+)['\"]?")
ASSIGNMENT = re.compile(r"[A-Za-z_][A-Za-z0-9_]*=.*")
WRAPPERS = {"sudo", "time", "nohup", "exec", "command", "env"}
SHELLS = {"sh", "bash", "zsh"}


def _without_heredocs(command: str) -> str:
    kept, end = [], None
    for line in command.split("\n"):
        if end is not None:
            if line.strip() == end:
                end = None
            continue
        kept.append(line)
        m = HEREDOC_START.search(line)
        if m:
            end = m.group(1)
    return "\n".join(kept)


def _argv(segment: str) -> tuple[list[str], list[str]]:
    """The command's tokens with leading VAR=value and wrappers removed, and the
    assignments that were removed."""
    try:
        tokens = shlex.split(segment)
    except ValueError:
        tokens = segment.split()
    env: list[str] = []
    while tokens and (ASSIGNMENT.fullmatch(tokens[0]) or tokens[0] in WRAPPERS):
        if "=" in tokens[0]:
            env.append(tokens[0])
        tokens = tokens[1:]
    return tokens, env


def _project(tokens: list[str], env: list[str]) -> str | None:
    """The compose project a command names, or None when it names none."""
    name = None
    for assignment in env:
        key, _, value = assignment.partition("=")
        if key == "COMPOSE_PROJECT_NAME":
            name = value
    for i, tok in enumerate(tokens):
        if tok in ("-p", "--project-name") and i + 1 < len(tokens):
            name = tokens[i + 1]
        elif tok.startswith("--project-name="):
            name = tok.split("=", 1)[1]
    return name


def _reason(segment: str) -> str | None:
    """Why this simple command is refused, or None if it may run."""
    tokens, env = _argv(segment)
    if not tokens:
        return None
    prog = tokens[0]

    if prog in SHELLS and "-c" in tokens:
        i = tokens.index("-c")
        return check(tokens[i + 1]) if i + 1 < len(tokens) else None

    isolated = _project(tokens, env) not in (None, DEFAULT_PROJECT)
    docker = prog in ("docker", "docker-compose")
    compose = prog == "docker-compose" or (prog == "docker" and "compose" in tokens)

    if compose and "down" in tokens and not isolated:
        after = tokens[tokens.index("down") + 1:]
        if any(t == "--volumes" or re.fullmatch(r"-[a-z]*v[a-z]*", t) for t in after):
            return ("`docker compose down -v` deletes the Postgres volume. "
                    "Use `make clean` (backs up first), or `-p jobtrack-verify` "
                    "for a throwaway stack.")

    if docker and "volume" in tokens and any(t in ("rm", "remove", "prune") for t in tokens):
        return ("removing Docker volumes can delete the database. "
                "Run `make db-backup` and remove it by hand if you mean it.")

    if docker and "system" in tokens and "prune" in tokens and "--volumes" in tokens:
        return "`docker system prune --volumes` deletes the database volume."

    # Joined text, not tokens: the seed command usually arrives quoted —
    # `tools "go run ./cmd/seed -reset"` — which shlex returns as one token.
    joined = " ".join(tokens)
    if (docker or prog == "go") and "cmd/seed" in joined and not isolated and re.search(
            r"(^|\s)-reset(\s|$)", joined):
        return "`seed -reset` deletes every posting. Use `make seed-reset` (backs up first)."

    return None


def check(command: str) -> str | None:
    for segment in SEPARATORS.split(_without_heredocs(command)):
        reason = _reason(segment.strip())
        if reason:
            return reason
    return None


CASES = [
    # (command, should_block)
    ("docker compose down -v", True),
    ("docker compose down --volumes --remove-orphans", True),
    ("docker compose down -v --remove-orphans 2>&1 | tail -2", True),
    ("cd /x && docker compose down -v", True),
    ("docker-compose down -v", True),
    ("sudo docker compose down -v", True),
    ('bash -c "docker compose down -v"', True),
    ("docker compose -p jobtrack down -v", True),
    ("COMPOSE_PROJECT_NAME=jobtrack docker compose down -v", True),
    ("docker volume rm jobtrack_pgdata", True),
    ("docker volume prune -f", True),
    ("docker system prune -a --volumes", True),
    ('docker compose run --rm tools "go run ./cmd/seed -reset -ingest"', True),
    ("go run ./cmd/seed -reset", True),
    ("git commit -F - <<'EOF'\nsafe prose\nEOF\ndocker compose down -v", True),
    # allowed
    ("docker compose -p jobtrack-verify down -v --remove-orphans", False),
    ("COMPOSE_PROJECT_NAME=jobtrack-verify docker compose down -v", False),
    ("docker compose down", False),
    ("docker compose down --remove-orphans", False),
    ('docker run --rm -v "$PWD:/src" alpine ls', False),
    ('docker compose -p jobtrack-verify run --rm tools "go run ./cmd/seed -reset"', False),
    ('docker compose run --rm tools "go run ./cmd/seed -ingest"', False),
    ("make clean", False),
    ("make db-reset", False),
    ("docker volume ls", False),
    ("git diff -v", False),
    ('git commit -m "never run docker compose down -v"', False),
    ("git commit -F - <<'EOF'\nIt was lost to a `docker compose down -v`.\n"
     'Also `tools "go run ./cmd/seed -reset"`.\nEOF', False),
    ('echo "docker volume rm is dangerous"', False),
    ('grep -n "down -v" Makefile', False),
]


def self_test() -> int:
    failed = 0
    for command, should_block in CASES:
        if (check(command) is not None) != should_block:
            failed += 1
            print(f"FAIL {'should block' if should_block else 'should allow'}: {command!r}")
    print(f"guard-destructive: {len(CASES) - failed}/{len(CASES)} cases pass")
    return 1 if failed else 0


def main() -> int:
    if "--self-test" in sys.argv:
        return self_test()
    try:
        event = json.load(sys.stdin)
    except json.JSONDecodeError:
        return 0  # Not ours to judge; never block on a parse failure.
    reason = check(event.get("tool_input", {}).get("command", ""))
    if reason:
        print(f"Blocked: {reason}", file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main())
