package httpx

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The first tests in this package, which CLAUDE.md's gap table names. They start
// here because problem.go is the one file whose output every API consumer sees,
// and because its type URI spent the project's whole life pointing at a domain
// somebody else owns.

// constructors is every APIError builder and the kind it claims. Adding one
// without adding it here is caught by the count assertion below.
func constructors() map[string]*APIError {
	return map[string]*APIError{
		"validation-failed": ErrBadRequest("detail"),
		"unauthorized":      ErrUnauthorized(),
		"not-found":         ErrNotFound(),
		"conflict":          ErrConflict("detail"),
		"payload-too-large": ErrTooLarge("detail"),
		"unprocessable":     ErrUnprocessable("detail"),
		"rate-limited":      ErrRateLimited("30"),
		"internal":          ErrInternal(os.ErrClosed),
		"unavailable":       ErrUnavailable("detail"),
	}
}

func TestConstructors_KindMatchesItsName(t *testing.T) {
	for want, err := range constructors() {
		if err.Kind != want {
			t.Errorf("constructor for %q produced Kind %q", want, err.Kind)
		}
		if err.Status < 400 || err.Status > 599 {
			t.Errorf("%s: status %d is not an error status", want, err.Status)
		}
		if err.Title == "" {
			t.Errorf("%s: no Title; a problem+json body with no title is not useful", want)
		}
	}
}

// A type URI SHOULD resolve to documentation (RFC 9457). Ours resolves to a
// section of api-design.md, so every kind needs an anchor there — otherwise the
// link lands on the top of a page that does not explain the error, which is the
// kind of half-truth that makes people stop following links.
func TestProblemKindsAreDocumented(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := os.ReadFile(filepath.Join(root, "docs", "architecture", "api-design.md"))
	if err != nil {
		t.Fatalf("read api-design.md: %v", err)
	}
	text := string(doc)

	for kind := range constructors() {
		anchor := `<a id="` + kind + `"></a>`
		if !strings.Contains(text, anchor) {
			t.Errorf("kind %q has no anchor in api-design.md; its type URI would resolve to "+
				"a page that does not describe it. Add %s", kind, anchor)
		}
	}
}

// The base must be a URL on a domain this project controls. The previous value
// made every error response point at jobtrack.dev, which resolves and belongs to
// somebody else.
func TestProblemBase_IsOursAndAbsolute(t *testing.T) {
	if !strings.HasPrefix(problemBase, "https://") {
		t.Fatalf("problemBase %q is not an absolute https URI", problemBase)
	}
	if strings.Contains(problemBase, "jobtrack.dev") {
		t.Errorf("problemBase points at jobtrack.dev, a domain this project does not own")
	}
	if !strings.Contains(problemBase, "github.com/ergodicregulus/jobtrack") {
		t.Errorf("problemBase %q is not on the project's own repository", problemBase)
	}
}

// The underlying error is logged, never sent. An internal detail in a response
// body is an information leak, and ErrInternal is the constructor most likely to
// be handed a database error carrying a query or a hostname.
func TestErrInternal_DoesNotLeakTheUnderlyingError(t *testing.T) {
	secret := "pgx: dial tcp 10.0.3.14:5432: connection refused"
	err := ErrInternal(errors.New(secret))

	if strings.Contains(err.Detail, secret) {
		t.Errorf("Detail leaks the underlying error: %q", err.Detail)
	}
	if strings.Contains(err.Title, secret) {
		t.Errorf("Title leaks the underlying error: %q", err.Title)
	}
	// It must still be recoverable for logging, or the trace ID buys nothing.
	if err.Unwrap() == nil {
		t.Error("the cause is not retrievable; it must be logged even though it is not sent")
	}
}
