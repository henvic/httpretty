// Package httpretty prints your HTTP requests pretty on your terminal screen.
// You can use this package both on the client-side and on the server-side.
//
// This package provides a better way to view HTTP traffic without httputil
// DumpRequest, DumpRequestOut, and DumpResponse heavy debugging functions.
//
// You can use the logger quickly to log requests you are opening. For example:
// 	package main
//
// 	import (
// 		"fmt"
// 		"net/http"
// 		"os"
//
// 		"github.com/henvic/httpretty"
// 	)
//
// 	func main() {
// 		logger := &httpretty.Logger{
// 			Time:           true,
// 			TLS:            true,
// 			RequestHeader:  true,
// 			RequestBody:    true,
// 			ResponseHeader: true,
// 			ResponseBody:   true,
// 			Colors:         true,
// 			Formatters:     []httpretty.Formatter{&httpretty.JSONFormatter{}},
// 		}
//
// 		http.DefaultClient.Transport = logger.RoundTripper(http.DefaultClient.Transport) // tip: you can use it on any *http.Client
//
// 		if _, err := http.Get("https://www.google.com/"); err != nil {
// 			fmt.Fprintf(os.Stderr, "%+v\n", err)
// 			os.Exit(1)
// 		}
// 	}
//
// If you pass nil to the logger.RoundTripper it is going to fallback to http.DefaultTransport.
//
// You can use the logger quickly to log requests on your server. For example:
// 	logger := &httpretty.Logger{
// 		Time:           true,
// 		TLS:            true,
// 		RequestHeader:  true,
// 		RequestBody:    true,
// 		ResponseHeader: true,
// 		ResponseBody:   true,
// 	}
//
// 	logger.Middleware(handler)
//
// Note: server logs doesn't include response headers set by the server.
// client logs doesn't include request headers set by the HTTP client.
package httpretty

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"sync"

	"github.com/henvic/httpretty/internal/color"
)

// Formatter can be used to format body.
//
// If the Format function returns an error, the content is printed in verbatum after a warning.
// Match receives a mediatype from the Content-Type field. The body is formatted if it returns true.
type Formatter interface {
	Match(mediatype string) bool
	Format(dst *bytes.Buffer, src []byte) error
}

// WithHide can be used to protect a request from being exposed.
func WithHide(ctx context.Context) context.Context {
	return context.WithValue(ctx, contextHide{}, struct{}{})
}

// Logger provides a way for you to print client and server-side information about your HTTP traffic.
type Logger struct {
	// SkipRequestInfo can be set to not print a line showing the request URI on all requests.
	// In addition, when used on the server-side a line containing the remote address is printed too.
	SkipRequestInfo bool

	// Time the request began and its duration.
	Time bool

	// TLS information, such as certificate and ciphers.
	// BUG(henvic): Currently, the TLS information prints after the response header, although it
	// should be printing before the request header.
	TLS bool

	// RequestHeader set by the client or received from the server.
	RequestHeader bool

	// RequestBody sent by the client or received by the server.
	RequestBody bool

	// ResponseHeader received by the client or set by the HTTP handlers.
	ResponseHeader bool

	// ResponseBody received by the client or set by the server.
	ResponseBody bool

	// SkipSanitize can be set to bypass sanitizing headers containing credentials (such as Authorization).
	SkipSanitize bool

	// Colors sets ANSI escape codes that terminals use to print text in different colors.
	Colors bool

	// Formatters for the request and response bodies.
	// No standard formatters are used. You need to add what you want to use explicitly.
	// We provide a JSONFormatter for convenience (add it manually).
	Formatters []Formatter

	// MaxRequestBody the logger can print.
	MaxRequestBody int64

	// MaxResponseBody the logger can print.
	MaxResponseBody int64

	mu      sync.Mutex // ensures atomic writes; protects the following fields
	w       io.Writer
	filter  Filter
	flusher Flusher
}

// Filter allows you to skip requests.
//
// If an error happens and you want to log it, you can pass a not-null error value.
type Filter func(req *http.Request) (skip bool, err error)

// Flusher defines how logger prints requests.
type Flusher int

// Logger can print without flushing, when they are available, or when the request is done.
const (
	// NoBuffer strategy prints anything immediately, without buffering.
	// It has the issue of mingling concurrent requests in unpredictable ways.
	NoBuffer Flusher = iota

	// OnReady buffers and prints each step of the request or response (header, body) whenever they are ready.
	// It reduces mingling caused by mingling but does not give any ordering guarantee, so responses can still be out of order.
	OnReady

	// OnEnd buffers the whole request and flushes it once, in the end.
	OnEnd
)

// SetFilter allows you to set a function to skip requests.
// Pass nil to remove all filters. This method is concurrency safe.
func (l *Logger) SetFilter(f Filter) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.filter = f
}

// SetOutput sets the output destination for the logger.
func (l *Logger) SetOutput(w io.Writer) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.w = w
}

// SetFlusher sets the flush strategy for the logger.
func (l *Logger) SetFlusher(f Flusher) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.flusher = f
}

func (l *Logger) getWriter() io.Writer {
	if l.w == nil {
		return os.Stdout
	}

	return l.w
}

func (l *Logger) getFilter() Filter {
	l.mu.Lock()
	f := l.filter
	defer l.mu.Unlock()
	return f
}

type contextHide struct{}

type roundTripper struct {
	logger *Logger
	rt     http.RoundTripper
}

// RoundTripper returns a RoundTripper that uses the logger.
func (l *Logger) RoundTripper(rt http.RoundTripper) http.RoundTripper {
	return roundTripper{
		logger: l,
		rt:     rt,
	}
}

// RoundTrip implements the http.RoundTrip interface.
func (r roundTripper) RoundTrip(req *http.Request) (resp *http.Response, err error) {
	tripper := r.rt

	if tripper == nil {
		// BUG(henvic): net/http data race condition when the client
		// does concurrent requests using the very same HTTP transport.
		// See Go standard library issue https://golang.org/issue/30597
		tripper = http.RoundTripper(http.DefaultTransport)
	}

	l := r.logger
	p := newPrinter(l)
	defer p.flush()

	if hide := req.Context().Value(contextHide{}); hide != nil || p.checkFilter(req) {
		return tripper.RoundTrip(req)
	}

	var tlsClientConfig *tls.Config

	if l.Time {
		defer p.printTimeRequest()()
	}

	if !l.SkipRequestInfo {
		p.printRequestInfo(req)
	}

	if transport, ok := tripper.(*http.Transport); ok && transport.TLSClientConfig != nil {
		tlsClientConfig = transport.TLSClientConfig

		if tlsClientConfig.InsecureSkipVerify {
			p.printf("* Skipping TLS verification: %s\n",
				p.format(color.FgRed, "connection is susceptible to man-in-the-middle attacks."))
		}
	}

	if l.TLS && tlsClientConfig != nil {
		// please remember http.Request.TLS is ignored by the HTTP client.
		p.printOutgoingClientTLS(tlsClientConfig)
	}

	p.printRequest(req)

	defer func() {
		if err != nil {
			p.printf("* %s\n", p.format(color.FgRed, err))

			if resp == nil {
				return
			}
		}

		p.printTLSInfo(resp.TLS, false)
		p.printTLSServer(req.Host, resp.TLS)
		p.printResponse(resp)
	}()

	return tripper.RoundTrip(req)
}

// Middleware for logging incoming requests to a HTTP server.
func (l *Logger) Middleware(next http.Handler) http.Handler {
	return httpHandler{
		logger: l,
		next:   next,
	}
}

type httpHandler struct {
	logger *Logger
	next   http.Handler
}

// ServeHTTP is a middleware for logging incoming requests to a HTTP server.
func (h httpHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	l := h.logger
	p := newPrinter(l)
	defer p.flush()

	if hide := req.Context().Value(contextHide{}); hide != nil || p.checkFilter(req) {
		h.next.ServeHTTP(w, req)
		return
	}

	if p.logger.Time {
		defer p.printTimeRequest()()
	}

	if !p.logger.SkipRequestInfo {
		p.printRequestInfo(req)
	}

	if p.logger.TLS {
		p.printTLSInfo(req.TLS, true)
		p.printIncomingClientTLS(req.TLS)
	}

	p.printRequest(req)

	rec := &responseRecorder{
		ResponseWriter: w,

		statusCode: http.StatusOK,

		maxReadableBody: l.MaxResponseBody,
		buf:             &bytes.Buffer{},
	}

	defer p.printServerResponse(req, rec)
	h.next.ServeHTTP(rec, req)
}

// PrintRequest prints a request, even when it is WithHide is used to hide it.
//
// It doesn't log TLS connection details or request duration.
func (l *Logger) PrintRequest(req *http.Request) {
	var p = printer{logger: l}

	if skip := p.checkFilter(req); skip {
		return
	}

	p.printRequest(req)
}

// PrintResponse prints a response.
func (l *Logger) PrintResponse(resp *http.Response) {
	var p = printer{logger: l}
	p.printResponse(resp)
}

// JSONFormatter helps you read unreadable JSON documents.
//
// github.com/tidwall/pretty could be used to add colors to it.
// However it would add an external dependency. If you want, you can define
// your own formatter using it or anything else. See Formatter interface.
type JSONFormatter struct{}

// Match JSON media type.
func (j *JSONFormatter) Match(mediatype string) bool {
	return mediatype == "application/json"
}

// Format JSON content.
func (j *JSONFormatter) Format(dst *bytes.Buffer, src []byte) error {
	if !json.Valid(src) {
		// We want to get the error of json.checkValid, not unmarshal it.
		// The happy path has been optimized, maybe prematurely.
		if err := json.Unmarshal(src, &json.RawMessage{}); err != nil {
			return err
		}
	}

	err := json.Indent(dst, src, "", "    ")
	return err
}