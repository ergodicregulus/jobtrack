// Command resume-parser turns uploaded PDF/DOCX files into structured JSON.
//
// This is the one genuinely network-isolated service, and it is isolated for
// security rather than scale: it is the only component that executes parsing
// logic over untrusted binary input. PDF parsers are a well-documented source
// of crashes, unbounded memory growth and parser-level exploits.
//
// Its deployment holds no database credentials and denies all network egress,
// so a successful exploit here reaches nothing. A crash returns a clean error
// to one user; the same crash inside the api process would take down request
// handling for everyone on that pod, next to session data and DB credentials.
package main

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/jobtrack/jobtrack/internal/app"
	"github.com/jobtrack/jobtrack/internal/httpx"
	"github.com/jobtrack/jobtrack/internal/normalise"
	"github.com/jobtrack/jobtrack/internal/resume"
)

func main() {
	ctx := context.Background()

	// NeedsDB is deliberately false. This process must not be able to reach the
	// database even if a future change tries to.
	a, err := app.New(ctx, app.Options{Service: "resume-parser", NeedsObject: true})
	if err != nil {
		app.Fatal(err)
	}

	// Built once. The vocabulary compiles ~113 regexes and is read-only, so
	// rebuilding it per request would be pure waste on the hot path.
	vocab := normalise.DefaultVocabulary()

	mux := http.NewServeMux()

	// The whole service. Bytes in, structured JSON out, no state, no database,
	// no egress: everything this process knows dies with the request, which is
	// what makes a compromise here reach nothing.
	mux.Handle("POST /parse", httpx.Wrap(a.Log, func(w http.ResponseWriter, r *http.Request) error {
		ctx := r.Context()

		// The body is already bounded by MaxBodyBytes below, so this cannot
		// grow without limit.
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return httpx.ErrBadRequest("could not read the uploaded file")
		}
		if len(body) == 0 {
			return httpx.ErrBadRequest("the uploaded file is empty")
		}

		text, format, err := resume.Extract(body)
		switch {
		case errors.Is(err, resume.ErrNoText):
			// Not a 500 and not a generic 400. This has a specific cause and
			// specific advice, and the advice is worth more than the error:
			// a file with no text layer fails most employer systems too.
			return httpx.ErrUnprocessable(
				"This file has no text in it — it is most likely a scan or an " +
					"image export. Applicant tracking systems cannot read it " +
					"either, so exporting a text-based PDF or a DOCX is worth " +
					"doing regardless of us.")
		case errors.Is(err, resume.ErrUnsupportedFormat):
			return httpx.ErrUnprocessable(
				"We cannot read this file type yet. DOCX and plain text work today.")
		case err != nil:
			// A malformed document is the user's file being broken, not our
			// bug. Saying so is more useful than a 500.
			a.Log.WarnContext(ctx, "resume extraction failed", "error", err, "format", format)
			return httpx.ErrUnprocessable(
				"We could not read this file. It may be password-protected or damaged.")
		}

		result := resume.Parse(text, format, vocab)

		// The extracted text goes back with the result so the caller can store
		// it encrypted. This process deliberately cannot: it holds no database
		// credentials, and giving it any would defeat the isolation.
		httpx.WriteJSON(ctx, w, a.Log, http.StatusOK, struct {
			resume.Result
			Text string `json:"text"`
		}{Result: result, Text: text})
		return nil
	}))
	mux.Handle("GET /livez", httpx.Wrap(a.Log, func(w http.ResponseWriter, r *http.Request) error {
		httpx.WriteJSON(r.Context(), w, a.Log, http.StatusOK, map[string]string{"status": "ok"})
		return nil
	}))
	mux.Handle("GET /readyz", httpx.Wrap(a.Log, func(w http.ResponseWriter, r *http.Request) error {
		httpx.WriteJSON(r.Context(), w, a.Log, http.StatusOK, map[string]string{"status": "ok"})
		return nil
	}))

	handler := httpx.Chain(mux,
		httpx.Tracing(a.Cfg.Service),
		httpx.RequestID(),
		httpx.Recover(a.Log),
		httpx.AccessLog(a.Log),
		// 5 MiB matches the upload limit. Bounding the body is the first line of
		// defence against a decompression bomb.
		httpx.MaxBodyBytes(5<<20),
	)

	srv := httpx.NewServer(httpx.ServerConfig{
		Addr:            a.Cfg.HTTPAddr,
		Handler:         handler,
		Log:             a.Log,
		ShutdownTimeout: a.Cfg.ShutdownTimeout,
	})

	a.Log.Info("resume-parser ready")

	if err := a.Run(srv.Run); err != nil {
		a.Log.Error("resume-parser stopped with error", "error", err)
		app.Fatal(err)
	}
}
