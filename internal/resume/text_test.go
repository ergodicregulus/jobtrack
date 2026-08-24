package resume

import (
	"archive/zip"
	"bytes"
	"errors"
	"strings"
	"testing"
)

// buildDOCX makes a minimal but structurally real Word package.
//
// Constructed rather than committed as a binary fixture: a checked-in .docx is
// opaque in review, and the thing under test is how we walk the XML, which is
// exactly what this makes visible.
func buildDOCX(t *testing.T, documentXML string, extra map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	write := func(name, body string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	write("[Content_Types].xml", `<?xml version="1.0"?><Types/>`)
	write("word/document.xml", documentXML)
	for k, v := range extra {
		write(k, v)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func para(runs ...string) string {
	var b strings.Builder
	b.WriteString("<w:p>")
	for _, r := range runs {
		b.WriteString("<w:r><w:t>" + r + "</w:t></w:r>")
	}
	b.WriteString("</w:p>")
	return b.String()
}

const docHeader = `<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`

func TestExtract_ReadsADOCX(t *testing.T) {
	doc := docHeader +
		para("Priya Raman") +
		para("priya@example.com") +
		para("Skills") +
		para("Go, PostgreSQL, Kubernetes") +
		`</w:body></w:document>`

	text, format, err := Extract(buildDOCX(t, doc, nil))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if format != FormatDOCX {
		t.Errorf("format = %q, want docx", format)
	}
	// Paragraphs must survive as lines: section detection is line-oriented, and
	// a run-together document has no headings at all.
	want := "Priya Raman\npriya@example.com\nSkills\nGo, PostgreSQL, Kubernetes"
	if text != want {
		t.Errorf("text =\n%q\nwant\n%q", text, want)
	}
}

// Splitting a word across runs is what Word does after a spell-check or a
// tracked change, and it is invisible in the document. Joining runs wrongly
// turns "PostgreSQL" into two tokens that match nothing.
func TestExtract_JoinsRunsSplitMidWord(t *testing.T) {
	doc := docHeader + para("Postgre", "SQL", " and Kuber", "netes") + `</w:body></w:document>`
	text, _, err := Extract(buildDOCX(t, doc, nil))
	if err != nil {
		t.Fatal(err)
	}
	if text != "PostgreSQL and Kubernetes" {
		t.Errorf("text = %q; runs inside a paragraph must join without a separator", text)
	}
}

// Tables are how a great many CVs lay out skills and dates. Running the cells
// together produces "GoPythonSQL", which matches nothing.
func TestExtract_KeepsTableCellsApart(t *testing.T) {
	doc := docHeader +
		`<w:tbl><w:tr>` +
		`<w:tc>` + para("Go") + `</w:tc>` +
		`<w:tc>` + para("PostgreSQL") + `</w:tc>` +
		`</w:tr></w:tbl>` +
		`</w:body></w:document>`

	text, _, err := Extract(buildDOCX(t, doc, nil))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "GoPostgreSQL") {
		t.Errorf("text = %q; table cells were run together", text)
	}
	if !strings.Contains(text, "Go") || !strings.Contains(text, "PostgreSQL") {
		t.Errorf("text = %q; lost a cell", text)
	}
}

// A file is identified by its first bytes, never its name. An attacker controls
// the extension; a parser handed the wrong format is a parser being fuzzed.
func TestDetectFormat_UsesMagicBytesNotNames(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want Format
	}{
		{"docx", []byte("PK\x03\x04rest of a zip"), FormatDOCX},
		{"pdf", []byte("%PDF-1.7\n..."), FormatPDF},
		{"text", []byte("Priya Raman\npriya@example.com\n"), FormatText},
		{"binary junk", []byte{0x00, 0x01, 0x02, 0xff, 0xfe}, FormatUnknown},
	}
	for _, c := range cases {
		if got := DetectFormat(c.in); got != c.want {
			t.Errorf("%s: DetectFormat = %q, want %q", c.name, got, c.want)
		}
	}
}

// A zip that is not a Word document must be refused rather than parsed as one.
func TestExtract_RefusesAZipThatIsNotAWordDocument(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("evil/word/document.xml")
	w.Write([]byte(docHeader + para("gotcha") + `</w:body></w:document>`))
	zw.Close()

	_, _, err := Extract(buf.Bytes())
	if !errors.Is(err, ErrUnsupportedFormat) {
		t.Errorf("err = %v, want ErrUnsupportedFormat — word/document.xml must be "+
			"matched exactly, not by suffix", err)
	}
}

// A scanned CV is a specific, useful diagnosis rather than a generic failure:
// such a file defeats most employer systems too.
func TestExtract_EmptyDocumentIsItsOwnError(t *testing.T) {
	doc := docHeader + `</w:body></w:document>`
	_, _, err := Extract(buildDOCX(t, doc, nil))
	if !errors.Is(err, ErrNoText) {
		t.Errorf("err = %v, want ErrNoText", err)
	}
}

// Word emits non-breaking spaces and smart punctuation constantly, and they
// defeat plain matching invisibly — the worst kind of parse failure, because
// the text looks correct on screen.
func TestExtract_NormalisesInvisibleCharacters(t *testing.T) {
	doc := docHeader +
		para("Go and​Python") +
		para("2019–2023") +
		`</w:body></w:document>`

	text, _, err := Extract(buildDOCX(t, doc, nil))
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(text, " ​–") {
		t.Errorf("text = %q still contains invisible or smart characters", text)
	}
	if !strings.Contains(text, "2019-2023") {
		t.Errorf("text = %q; an en dash in a date range must normalise to a hyphen "+
			"or the date parser will not see it", text)
	}
}

// A file claiming to be a PDF but holding nothing usable must fail with a
// clear error rather than silently producing rubble that corrupts every score
// downstream. Real PDF behaviour is covered in pdf_test.go, which needs the
// poppler binary; this one only checks identification and the failure shape.
func TestExtract_MalformedPDFIsIdentifiedAndRefused(t *testing.T) {
	_, format, err := Extract([]byte("%PDF-1.7\nstream stuff"))
	if format != FormatPDF {
		t.Errorf("format = %q, want pdf — the file must still be identified", format)
	}
	if err == nil {
		t.Fatal("a malformed PDF parsed successfully")
	}
}
