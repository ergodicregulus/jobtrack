package source

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DefaultMaxBody caps a single response. Boards are text; anything larger is a
// vendor bug or a redirect to something that is not a board, and reading it
// into memory unbounded is how one bad source takes down the ingestor.
const DefaultMaxBody = 16 << 20

// Request describes one conditional fetch of a board.
//
// This type exists because the first four adapters each wrote the same eighty
// lines — build request, set validators, classify status, cap and hash the body
// — and by the time there were three copies of parseRetryAfter they had already
// drifted: SmartRecruiters' copy dropped the TrimSpace and used `> 0` where the
// others used `>= 0`, so a `Retry-After: " 30 "` produced no backoff there and a
// 30-second one everywhere else. Nobody changed it on purpose. That is the
// argument for this file, and it is the same argument as ADR-0014's.
type Request struct {
	Method string // GET when empty.
	URL    string
	Body   []byte // Workday POSTs its query.

	// Header entries are set after the defaults, so an adapter can override
	// Accept or add a vendor-specific header without rebuilding the request.
	Header    map[string]string
	UserAgent string

	// MaxBody defaults to DefaultMaxBody.
	MaxBody int64
}

// Response is one classified fetch.
//
// Body is nil exactly when NotModified is true — there is nothing to parse and
// no caller should try.
type Response struct {
	Body        []byte
	StatusCode  int
	ETag        string
	LastModified string
	ContentHash  []byte
	NotModified bool

	// RetryAfterHeader is the raw header, set only on a 429. Kept raw rather
	// than parsed so Do stays a classifier and the caller decides what to do
	// with an absent or nonsensical value.
	RetryAfterHeader string
}

// Do performs a conditional request and classifies the result.
//
// It returns the vendor-agnostic sentinel errors, so callers get ErrSourceGone
// and ErrRateLimited without each deciding for itself which status codes mean
// what. `vendor` only prefixes error messages.
//
// NotModified is set by EITHER a 304 or a body that hashes to what we already
// have. Both matter: most vendors honour one validator or the other, and
// several honour neither while still serving a byte-identical board.
func Do(ctx context.Context, client HTTPDoer, vendor Vendor, src Source, r Request) (Response, error) {
	req, err := buildRequest(ctx, src, r)
	if err != nil {
		return Response{}, fmt.Errorf("%s: build request: %w", vendor, err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return Response{}, fmt.Errorf("%s: fetch %s: %w", vendor, src.BoardToken, err)
	}
	defer func() {
		// Drain before closing so the connection returns to the pool. Without
		// it every poll opens a new TCP connection.
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	out, err := classify(vendor, src, resp)
	if err != nil || out.NotModified {
		return out, err
	}

	max := r.MaxBody
	if max <= 0 {
		max = DefaultMaxBody
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, max))
	if err != nil {
		return out, fmt.Errorf("%s: read body: %w", vendor, err)
	}

	// The second line of change detection. Several vendors serve a
	// byte-identical board with no ETag and no Last-Modified, and hashing lets
	// us skip parsing and every downstream job anyway.
	sum := sha256.Sum256(raw)
	out.ContentHash = sum[:]
	if len(src.ContentHash) == len(sum) && string(src.ContentHash) == string(sum[:]) {
		out.NotModified = true
		return out, nil
	}
	out.Body = raw
	return out, nil
}

// buildRequest applies the defaults, then the validators, then the caller's
// headers — in that order, so an adapter can override any of them.
func buildRequest(ctx context.Context, src Source, r Request) (*http.Request, error) {
	method := r.Method
	if method == "" {
		method = http.MethodGet
	}
	var body io.Reader
	if len(r.Body) > 0 {
		body = bytes.NewReader(r.Body)
	}

	req, err := http.NewRequestWithContext(ctx, method, r.URL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", r.UserAgent)

	// Validators go on GETs only. A POST body defines the resource here, so a
	// 304 against it would be answering a question we did not ask.
	if method == http.MethodGet {
		if src.ETag != "" {
			req.Header.Set("If-None-Match", src.ETag)
		}
		if src.LastModified != "" {
			req.Header.Set("If-Modified-Since", src.LastModified)
		}
	}
	for k, v := range r.Header {
		req.Header.Set(k, v)
	}
	return req, nil
}

// classify turns a status code into either a sentinel error or a Response.
//
// Separated from Do so the mapping is one readable table rather than a switch
// buried between I/O. Adapters get ErrSourceGone and ErrRateLimited without
// each deciding for itself which codes mean what.
func classify(vendor Vendor, src Source, resp *http.Response) (Response, error) {
	out := Response{
		StatusCode:   resp.StatusCode,
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
	}

	switch {
	case resp.StatusCode == http.StatusNotModified:
		out.NotModified = true
		// Carry the previous validators forward: a 304 need not repeat them,
		// and dropping them turns every subsequent poll into a full fetch.
		out.ETag = FirstNonEmpty(out.ETag, src.ETag)
		out.LastModified = FirstNonEmpty(out.LastModified, src.LastModified)
		out.ContentHash = src.ContentHash
		return out, nil

	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return out, fmt.Errorf("%s: board %q: %w", vendor, src.BoardToken, ErrSourceGone)

	case resp.StatusCode == http.StatusTooManyRequests:
		out.RetryAfterHeader = resp.Header.Get("Retry-After")
		return out, fmt.Errorf("%s: board %q: %w", vendor, src.BoardToken, ErrRateLimited)

	case resp.StatusCode >= 400:
		return out, fmt.Errorf("%s: board %q: unexpected status %d", vendor, src.BoardToken, resp.StatusCode)
	}
	return out, nil
}

// ParseRetryAfter reads a Retry-After header, which RFC 9110 allows to be
// either a delay in seconds or an HTTP date.
//
// Zero means "no usable value" and callers apply their own backoff. A date in
// the past is one of those: it is a server telling us to retry at a time that
// has already gone, which is not a delay.
func ParseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// FirstNonEmpty returns the first value that is not blank once trimmed.
//
// Vendors send "" and "   " interchangeably for an absent field, so a plain
// `!= ""` check lets whitespace win over a real value further down the list.
func FirstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}
