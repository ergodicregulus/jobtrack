"""Minimal structural reader for gofmt'd Go source.

Three of the architecture checks need the same three facts about a Go file:
what it imports, where its string literals are, and where each top-level
function starts and ends. A real parser would mean a Go toolchain on the host,
and these checks are meant to run before `make dev` has ever been used.

What makes a regex approach honest here rather than a guess: gofmt is enforced
by `make fmt-check`, and it guarantees the two properties this relies on — a
top-level declaration starts at column 0, and its closing brace is a lone `}`
at column 0. Comments and string bodies are blanked before any structural scan,
so a `func` inside a doc comment or a SQL keyword inside a template literal
cannot be mistaken for code.
"""

from __future__ import annotations

import re
from dataclasses import dataclass, field
from pathlib import Path

# Directories whose Go files are not ours to police.
SKIP_PARTS = {"node_modules", ".git", "vendor", "testdata", "build", ".svelte-kit"}


@dataclass(frozen=True)
class Literal:
    line: int
    text: str


@dataclass(frozen=True)
class Func:
    name: str
    start: int  # 1-indexed line of `func`
    end: int    # 1-indexed line of the closing brace

    @property
    def lines(self) -> int:
        return self.end - self.start + 1


@dataclass
class GoFile:
    path: Path
    package: str = ""
    # Source with comments and string bodies blanked, newlines preserved. Any
    # structural scan must use this rather than the raw text, or a keyword in a
    # doc comment reads as code.
    code: str = ""
    imports: list[str] = field(default_factory=list)
    literals: list[Literal] = field(default_factory=list)
    funcs: list[Func] = field(default_factory=list)

    @property
    def rel(self) -> str:
        return str(self.path)

    @property
    def is_test(self) -> bool:
        return self.path.name.endswith("_test.go")


_FUNC = re.compile(r"^func\s+(?:\([^)]*\)\s*)?([A-Za-z_][A-Za-z0-9_]*)")
_PACKAGE = re.compile(r"^package\s+([A-Za-z_][A-Za-z0-9_]*)", re.M)
_IMPORT_ONE = re.compile(r'^import\s+(?:[.\w]+\s+)?"([^"]+)"', re.M)
_IMPORT_BLOCK = re.compile(r"^import\s*\((.*?)^\)", re.M | re.S)
_IMPORT_LINE = re.compile(r'^\s*(?:[.\w]+\s+)?"([^"]+)"', re.M)


def _blank(src: str) -> tuple[str, str, list[Literal]]:
    """Return (code, decommented, literals).

    `code` has both comments and string bodies replaced by spaces: a `func`
    line inside a raw-string template must not read as a declaration.
    `decommented` keeps string bodies, because an import path *is* a string
    literal and blanking it would make every import invisible.

    Newlines are preserved in both, so a line number computed against either is
    still a line number in the original file.
    """
    out = list(src)
    keep = list(src)
    lits: list[Literal] = []
    i, n = 0, len(src)
    line = 1

    def blank_range(a: int, b: int, both: bool = True) -> None:
        for k in range(a, min(b, n)):
            if out[k] != "\n":
                out[k] = " "
                if both:
                    keep[k] = " "

    while i < n:
        c = src[i]
        if c == "\n":
            line += 1
            i += 1
        elif c == "/" and i + 1 < n and src[i + 1] == "/":
            j = src.find("\n", i)
            j = n if j < 0 else j
            blank_range(i, j)
            i = j
        elif c == "/" and i + 1 < n and src[i + 1] == "*":
            j = src.find("*/", i + 2)
            j = n if j < 0 else j + 2
            line += src.count("\n", i, j)
            blank_range(i, j)
            i = j
        elif c == "`":
            j = src.find("`", i + 1)
            j = n if j < 0 else j + 1
            lits.append(Literal(line, src[i + 1 : j - 1]))
            line += src.count("\n", i, j)
            blank_range(i + 1, j - 1, both=False)
            i = j
        elif c == '"':
            j = i + 1
            while j < n and src[j] not in ('"', "\n"):
                j += 2 if src[j] == "\\" else 1
            lits.append(Literal(line, src[i + 1 : j]))
            blank_range(i + 1, j, both=False)
            i = j + 1
        elif c == "'":
            j = i + 1
            while j < n and src[j] not in ("'", "\n"):
                j += 2 if src[j] == "\\" else 1
            i = j + 1
        else:
            i += 1
    return "".join(out), "".join(keep), lits


def read(path: Path) -> GoFile:
    src = path.read_text(errors="replace")
    code, decommented, lits = _blank(src)
    gf = GoFile(path=path, literals=lits, code=code)

    if m := _PACKAGE.search(code):
        gf.package = m.group(1)

    gf.imports = _IMPORT_ONE.findall(decommented)
    for block in _IMPORT_BLOCK.findall(decommented):
        gf.imports.extend(_IMPORT_LINE.findall(block))

    lines = code.splitlines()
    start: int | None = None
    name = ""
    for idx, ln in enumerate(lines):
        if ln.startswith("func "):
            # A `func` line already open means the previous one had no lone
            # closing brace — a one-line func literal assignment, not a decl.
            start, name = idx, (_FUNC.match(ln).group(1) if _FUNC.match(ln) else "?")
        elif ln == "}" and start is not None:
            gf.funcs.append(Func(name, start + 1, idx + 1))
            start = None
    return gf


def walk(root: Path = Path(".")) -> list[GoFile]:
    files = []
    for p in sorted(root.rglob("*.go")):
        if SKIP_PARTS & set(p.parts):
            continue
        files.append(read(p))
    return files
