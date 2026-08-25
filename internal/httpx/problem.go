// Package httpx holds HTTP transport concerns: error shape, middleware, and
// the server lifecycle.
package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jobtrack/jobtrack/internal/telemetry"
)

// Problem is an RFC 9457 problem detail.
//
// One error shape for the whole API. A handler that writes a bare string or a
// bespoke JSON object forces every client to special-case it, and clients
// respond by not handling errors at all.
type Problem struct {
	Type     string       `json:"type"`
	Title    string       `json:"title"`
	Status   int          `json:"status"`
	Detail   string       `json:"detail,omitempty"`
	Instance string       `json:"instance,omitempty"`
	Errors   []FieldError `json:"errors,omitempty"`
	// TraceID is present on every error. A user can paste it into a support
	// message and we can find the exact request, its logs and its queries.
	TraceID string `json:"trace_id,omitempty"`
}

type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

const problemBase = "https://jobtrack.dev/problems/"

// APIError is an error that carries an HTTP representation. Handlers return
// these; the writer turns them into responses.
type APIError struct {
	Status int
	Kind   string
	Title  string
	Detail string
	Fields []FieldError
	// Err is the underlying cause. Logged, never sent to the client — internal
	// details in an error body are an information leak.
	Err error
}

func (e *APIError) Error() string {
	if e.Err != nil {
		return e.Kind + ": " + e.Err.Error()
	}
	return e.Kind + ": " + e.Detail
}

func (e *APIError) Unwrap() error { return e.Err }

// Constructors for the errors we actually return. Having these as functions
// rather than inline literals is what keeps titles and types consistent across
// dozens of handlers.

func ErrBadRequest(detail string, fields ...FieldError) *APIError {
	return &APIError{Status: http.StatusBadRequest, Kind: "validation-failed",
		Title: "Validation failed", Detail: detail, Fields: fields}
}

func ErrUnauthorized() *APIError {
	return &APIError{Status: http.StatusUnauthorized, Kind: "unauthorized",
		Title: "Authentication required"}
}

// ErrNotFound is returned both when something does not exist and when it exists
// but is not visible to this user. Distinguishing them would let an
// unauthenticated caller enumerate valid IDs.
func ErrNotFound() *APIError {
	return &APIError{Status: http.StatusNotFound, Kind: "not-found",
		Title: "Not found"}
}

func ErrConflict(detail string) *APIError {
	return &APIError{Status: http.StatusConflict, Kind: "conflict",
		Title: "Conflict", Detail: detail}
}

func ErrTooLarge(detail string) *APIError {
	return &APIError{Status: http.StatusRequestEntityTooLarge, Kind: "payload-too-large",
		Title: "Payload too large", Detail: detail}
}

func ErrUnprocessable(detail string) *APIError {
	return &APIError{Status: http.StatusUnprocessableEntity, Kind: "unprocessable",
		Title: "Unprocessable request", Detail: detail}
}

func ErrRateLimited(retryAfter string) *APIError {
	return &APIError{Status: http.StatusTooManyRequests, Kind: "rate-limited",
		Title: "Too many requests", Detail: "Retry after " + retryAfter}
}

func ErrInternal(err error) *APIError {
	return &APIError{Status: http.StatusInternalServerError, Kind: "internal",
		Title: "Internal server error", Err: err}
}

func ErrUnavailable(detail string) *APIError {
	return &APIError{Status: http.StatusServiceUnavailable, Kind: "unavailable",
		Title: "Service unavailable", Detail: detail}
}

// WriteProblem renders err as a problem+json response and logs it.
//
// Logging happens here — at the boundary — and nowhere else on the error path.
// Logging at every frame produces five lines per failure and no more
// information than one line at the edge.
func WriteProblem(ctx context.Context, w http.ResponseWriter, r *http.Request, log *slog.Logger, err error) {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		apiErr = ErrInternal(err)
	}

	p := Problem{
		Type:     problemBase + apiErr.Kind,
		Title:    apiErr.Title,
		Status:   apiErr.Status,
		Detail:   apiErr.Detail,
		Instance: r.URL.Path,
		Errors:   apiErr.Fields,
		TraceID:  telemetry.TraceIDFromContext(ctx),
	}

	// 5xx means we broke; 4xx means the caller did. Only the former needs a
	// human, so only the former is an Error.
	attrs := []any{
		"status", apiErr.Status,
		"kind", apiErr.Kind,
		"method", r.Method,
		"path", r.URL.Path,
	}
	if apiErr.Status >= 500 {
		// The cause is logged, never returned: internal detail in a response
		// body is an information leak.
		log.ErrorContext(ctx, "request failed", append(attrs, "error", apiErr.Error())...)
		p.Detail = "" // never leak internals to the client
	} else {
		log.DebugContext(ctx, "request rejected", attrs...)
	}

	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(apiErr.Status)
	if encErr := json.NewEncoder(w).Encode(p); encErr != nil {
		log.ErrorContext(ctx, "failed to write problem response", "error", encErr)
	}
}

// WriteJSON renders v as JSON with the given status.
func WriteJSON(ctx context.Context, w http.ResponseWriter, log *slog.Logger, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// The status line is already sent, so this cannot become an error
		// response. Log it and move on.
		log.ErrorContext(ctx, "failed to encode response", "error", err)
	}
}

// Handler is a handler that can fail. Returning an error rather than writing
// one means every handler gets consistent logging and error shape for free.
type Handler func(w http.ResponseWriter, r *http.Request) error

// Wrap adapts a Handler into http.Handler.
func Wrap(log *slog.Logger, h Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			WriteProblem(r.Context(), w, r, log, err)
		}
	})
}
