// Package resume turns an uploaded CV into structured, correctable data.
//
// The whole point is arithmetic: skills are 40 of the 100 points in every match
// score, and a user typing them by hand lists five. A parsed CV yields twenty,
// so this is the single largest available improvement to match quality.
//
// Three rules run through everything here, all from
// [ADR-0007](../../docs/architecture/adr/0007-resume-parsing-local-first.md):
//
//  1. **Deterministic.** No model, no network. The same file parses to the same
//     result every time, which is the only way a regression corpus works — and
//     a parser without a regression corpus decays.
//  2. **The user sees and corrects everything.** A silently wrong skill list is
//     worse than no skill list, because it corrupts every score downstream
//     while looking like it worked.
//  3. **Never guess silently.** Every extracted field carries how confident we
//     are and why, and a low-confidence parse says so rather than presenting
//     rubble as data.
package resume

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Format is an input file type we recognise.
type Format string

const (
	FormatDOCX    Format = "docx"
	FormatPDF     Format = "pdf"
	FormatText    Format = "text"
	FormatUnknown Format = "unknown"
)

// maxExtractedBytes bounds the text we will hold from one file.
//
// A 5 MiB DOCX is a zip, and a zip of highly repetitive XML expands enormously
// — the classic decompression bomb. The upload limit bounds the COMPRESSED
// size and says nothing about the decompressed one, so the bound has to be
// applied here as well. 4 MiB of text is roughly 800 pages; no real CV is
// within two orders of magnitude of it.
const maxExtractedBytes = 4 << 20

// ErrNoText means the file parsed cleanly and contained no text at all.
//
// A separate error because it has a specific, useful cause: the CV is a scan,
// or an image export. That is worth telling the user plainly, since such a file
// fails most employer ATS parsers too — "export a text-based PDF" is real
// advice rather than an apology.
var ErrNoText = errors.New("resume: no text layer in the document")

// ErrUnsupportedFormat is returned for a file we cannot read at all.
var ErrUnsupportedFormat = errors.New("resume: unsupported file format")

// DetectFormat identifies a file by its MAGIC BYTES, not its filename.
//
// An attacker controls the extension and the declared content type; they do not
// control the first four bytes as easily, and a parser handed the wrong format
// is a parser being fuzzed. The name is never consulted.
func DetectFormat(b []byte) Format {
	switch {
	case len(b) >= 4 && bytes.Equal(b[:4], []byte("PK\x03\x04")):
		// A zip. DOCX is the only zip we accept, and Extract verifies that by
		// looking for word/document.xml rather than trusting this.
		return FormatDOCX
	case len(b) >= 5 && bytes.Equal(b[:5], []byte("%PDF-")):
		return FormatPDF
	case utf8.Valid(b) && !bytes.ContainsRune(b, 0):
		// Plain text or Markdown. Accepted because it is trivially safe and
		// some people genuinely keep a CV that way.
		return FormatText
	}
	return FormatUnknown
}

// Extract pulls the text layer out of an uploaded file.
//
// It returns text only; understanding it is [Parse]'s job. Splitting the two is
// what lets the section and skill logic be tested on strings without a binary
// fixture for every case.
func Extract(b []byte) (string, Format, error) {
	format := DetectFormat(b)
	switch format {
	case FormatDOCX:
		text, err := extractDOCX(b)
		return text, format, err
	case FormatText:
		if len(b) > maxExtractedBytes {
			b = b[:maxExtractedBytes]
		}
		text := normaliseWhitespace(string(b))
		if strings.TrimSpace(text) == "" {
			return "", format, ErrNoText
		}
		return text, format, nil
	case FormatPDF:
		text, err := extractPDF(context.Background(), b)
		return text, format, err
	}
	return "", format, ErrUnsupportedFormat
}

// pdfTimeout bounds one extraction.
//
// A CV is one to three pages and takes milliseconds. Anything approaching this
// is a malformed or hostile file, and the whole reason the work happens in a
// child process is that such a file can be killed without taking the service
// with it.
const pdfTimeout = 20 * time.Second

// pdfMaxPages bounds the work regardless of how many pages the file claims.
//
// Ten is generous for a CV and cheap insurance against a document whose page
// tree is a bomb.
const pdfMaxPages = 10

// extractPDF shells out to pdftotext.
//
// **Not in-process, and not a Go library.** A PDF stores positioned glyphs on a
// canvas rather than text, so reading one means implementing xref tables,
// stream filters, font encodings and CMaps. Getting any of those subtly wrong
// produces text that looks plausible and is wrong — which silently corrupts
// every score the user sees afterwards, the worst failure this pipeline has.
// poppler is the most exercised implementation of that job in existence.
//
// A subprocess is also strictly safer than a library here: this is untrusted
// binary input, and a crash, a hang or a memory blow-up dies with the child
// instead of the service. That is the same argument that put this code in an
// isolated deployment in the first place, applied one level further in.
//
// Streams through stdin and stdout, so nothing is ever written to disk and the
// container filesystem can stay read-only.
func extractPDF(ctx context.Context, b []byte) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, pdfTimeout)
	defer cancel()

	path, cleanup, err := scratchPDF(b)
	if err != nil {
		return "", err
	}
	defer cleanup()

	// NO -layout, and this is a measured decision rather than a default.
	//
	// -layout reproduces the VISUAL arrangement, which for a two-column CV
	// interleaves the sidebar into the body — "Priya Raman   Summary", "Go
	// PostgreSQL" beside prose — and is precisely the mangling that defeats
	// parsing. Reading order, which is what plain pdftotext emits, keeps the
	// sidebar whole and then the body whole. Measured on a real two-column CV:
	// reading order recovered every skill and both dated roles; -layout
	// recovered neither section cleanly. See ADR-0012.
	//
	// Deliberately NOT -q. poppler's quiet mode suppresses the syntax errors as
	// well as the warnings, and those errors are the only thing that
	// distinguishes "this file is a scan" from "these bytes arrived truncated"
	// — a distinction that cost an hour here and reached the right answer only
	// once the messages were visible. stderr is bounded below, so keeping it is
	// free.
	cmd := exec.CommandContext(ctx, "pdftotext",
		"-enc", "UTF-8", // the default is Latin-1, which mangles any non-ASCII name
		"-l", strconv.Itoa(pdfMaxPages),
		path, "-", // file in, stdout out
	)

	var out, errBuf bytes.Buffer
	// Bounded exactly as the DOCX path is: the input limit says nothing about
	// how much text a file can expand into.
	cmd.Stdout = &limitedWriter{w: &out, n: maxExtractedBytes}
	cmd.Stderr = &limitedWriter{w: &errBuf, n: 4 << 10}

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("resume: PDF extraction timed out after %s", pdfTimeout)
		}
		return "", pdftotextError(err, &errBuf)
	}

	text := normaliseWhitespace(out.String())
	if strings.TrimSpace(text) == "" {
		// A valid PDF that yields nothing has one overwhelmingly likely cause,
		// and it is worth saying: the pages are images.
		return "", ErrNoText
	}
	return text, nil
}

// scratchPDF writes the bytes to a real file and returns its path.
//
// A real file, not a pipe. A PDF's cross-reference table lives at the END of the
// file, so a reader has to seek backwards before it can find anything. Handed a
// pipe, poppler buffers the stream itself and — in this container, under the
// service, though not in a one-shot run — failed with a bare exit 1 and no
// message at all. A file it can seek removes the ambiguity entirely, and costs
// one write of at most 5 MB.
//
// The deployment therefore needs a writable temp dir. That is a tmpfs mount, not
// a writable root: the filesystem stays read-only, and the scratch space is
// small, in memory, and gone when the pod restarts.
//
// The returned cleanup must run on every path. The file holds someone's CV, and
// this service exists precisely so that such data lives as briefly as possible.
func scratchPDF(b []byte) (path string, cleanup func(), err error) {
	dir, err := os.MkdirTemp("", "jt-resume-*")
	if err != nil {
		return "", nil, fmt.Errorf("resume: no writable scratch space: %w", err)
	}
	cleanup = func() { _ = os.RemoveAll(dir) }

	path = filepath.Join(dir, "in.pdf")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("resume: writing scratch file: %w", err)
	}
	return path, cleanup, nil
}

// pdftotextError turns a failed run into something a reader can act on.
func pdftotextError(err error, errBuf *bytes.Buffer) error {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		// The exit code is in the message on purpose. poppler's stderr is often
		// empty, so without the code a failure here is indistinguishable from
		// any other and the log line says nothing a reader can act on.
		return fmt.Errorf("resume: this PDF could not be read (pdftotext exit %d): %s",
			exitErr.ExitCode(), strings.TrimSpace(errBuf.String()))
	}
	// Not an exit status: the binary is missing from the image.
	return fmt.Errorf("resume: pdftotext unavailable: %w", err)
}

// limitedWriter caps what a child process can hand back.
//
// io.LimitReader cannot be used on the far side of an exec pipe, and an
// unbounded buffer fed by a subprocess is how a decompression bomb becomes an
// out-of-memory kill. Writes past the limit are discarded rather than erroring,
// so a merely long document still yields its first four megabytes.
type limitedWriter struct {
	w io.Writer
	n int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.n <= 0 {
		return len(p), nil
	}
	if len(p) > l.n {
		p = p[:l.n]
	}
	n, err := l.w.Write(p)
	l.n -= n
	return len(p), err
}

// extractDOCX reads word/document.xml out of the package.
//
// Stdlib only — a DOCX is a zip of XML, so archive/zip and encoding/xml are the
// whole toolchain. A dependency here would buy nothing but a supply chain.
func extractDOCX(b []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return "", fmt.Errorf("resume: not a readable zip: %w", err)
	}

	var doc *zip.File
	for _, f := range zr.File {
		// Exact match, not a suffix test. A crafted archive can contain
		// "evil/word/document.xml", and picking that would mean parsing a file
		// the Word format never intended to be the document.
		if f.Name == "word/document.xml" {
			doc = f
			break
		}
	}
	if doc == nil {
		return "", fmt.Errorf("%w: zip is not a Word document", ErrUnsupportedFormat)
	}

	rc, err := doc.Open()
	if err != nil {
		return "", fmt.Errorf("resume: opening document.xml: %w", err)
	}
	defer rc.Close()

	// LimitReader, not doc.UncompressedSize64. The declared size lives in the
	// archive's own header and is attacker-controlled; the only trustworthy
	// bound is the one applied while reading.
	text, err := docxText(io.LimitReader(rc, maxExtractedBytes))
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(text) == "" {
		return "", ErrNoText
	}
	return text, nil
}

// docxText walks WordprocessingML and emits plain text with the layout that
// matters preserved.
//
// Streaming rather than unmarshalling into a struct: WordprocessingML nests
// deeply and irregularly (runs inside hyperlinks inside smart tags inside
// paragraphs), so a struct that models it is either enormous or wrong. Only
// four element names actually matter, and a token loop reads them all
// regardless of how they are nested.
func docxText(r io.Reader) (string, error) {
	dec := xml.NewDecoder(r)
	var out strings.Builder

	// Text inside a run, accumulated so an empty paragraph can be told from one
	// whose runs all happened to be empty.
	var para strings.Builder
	inText := false

	flushParagraph := func() {
		if s := strings.TrimRight(para.String(), " \t"); s != "" {
			out.WriteString(s)
			out.WriteByte('\n')
		}
		para.Reset()
	}

	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// Truncation at the byte limit lands here. Whatever was read before
			// it is still useful, so the text wins over the error.
			if out.Len() > 0 || para.Len() > 0 {
				break
			}
			return "", fmt.Errorf("resume: malformed document.xml: %w", err)
		}

		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				inText = true
			case "tab":
				// A tab inside a run is usually a column gap in a CV's
				// "Company ....... 2019-2023" line. A space would merge two
				// fields into one; a tab keeps them separable later.
				para.WriteByte('\t')
			case "br", "cr":
				para.WriteByte('\n')
			}

		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				// Paragraph break. This is the layout signal that survives into
				// section detection — headings are paragraphs.
				flushParagraph()
			case "tc":
				// End of a table cell. Tables are how a great many CVs lay out
				// skills and dates, and running cells together produces
				// "GoPythonSQL". A tab keeps the boundary.
				para.WriteByte('\t')
			case "tr":
				flushParagraph()
			}

		case xml.CharData:
			if inText {
				para.Write(t)
			}
		}
	}
	flushParagraph()

	return normaliseWhitespace(out.String()), nil
}

// normaliseWhitespace makes downstream matching line-oriented and predictable.
//
// Bullets become plain lines: a CV's "•", "▪" and "-" all mean the same thing,
// and leaving them in means every later regex has to know about them.
func normaliseWhitespace(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	// Non-breaking and zero-width characters come through copy-paste and from
	// Word itself, and they defeat a plain " " match invisibly — the worst kind
	// of parse failure, because the text looks correct on screen.
	s = strings.NewReplacer(
		"\u00a0", " ", // non-breaking space
		"\u200b", "", // zero-width space
		"\u200c", "", // zero-width non-joiner
		"\ufeff", "", // byte-order mark
		"\u2013", "-", // en dash, ubiquitous in date ranges
		"\u2014", "-", // em dash
		"\u2018", "'", "\u2019", "'", // smart quotes
		"\u201c", `"`, "\u201d", `"`,
	).Replace(s)

	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		line = strings.TrimLeft(line, "\u2022\u25aa\u25e6\u2023\u00b7*- \t")
		line = strings.TrimSpace(line)
		out = append(out, line)
	}

	// Collapse runs of blank lines to one. A blank line is a meaningful
	// separator for section detection; five of them are not five separators.
	var b strings.Builder
	blank := false
	for _, line := range out {
		if line == "" {
			if !blank {
				b.WriteByte('\n')
			}
			blank = true
			continue
		}
		blank = false
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}
