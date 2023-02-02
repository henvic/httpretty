package httpretty

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/tools/txtar"
)

type helloHandler struct{}

func (h helloHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header()["Date"] = nil
	fmt.Fprintf(w, "Hello, world!")
}

var (
	//go:embed testdata/log.txtar
	dump  []byte
	dumpA = txtar.Parse(dump)
)

func golden(name string) string {
	for _, f := range dumpA.Files {
		if name == f.Name {
			return string(f.Data)
		}
	}
	panic("golden file not found")
}

func TestOutgoing(t *testing.T) {
	// important: cannot be in parallel because we are capturing os.Stdout
	ts := httptest.NewServer(&helloHandler{})
	defer ts.Close()
	logger := &Logger{
		TLS:            true,
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	}
	client := &http.Client{
		// Only use the default transport (http.DefaultTransport) on TestOutgoing.
		// Passing nil here = http.DefaultTransport.
		Transport: logger.RoundTripper(nil),
	}
	// code for capturing stdout was copied from the Go source code file src/testing/run_example.go:
	// https://github.com/golang/go/blob/ac56baa/src/testing/run_example.go
	stdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		panic(err)
	}
	os.Stdout = w
	outC := make(chan string)
	go func() {
		var buf strings.Builder
		_, errcp := io.Copy(&buf, r)
		r.Close()
		if errcp != nil {
			panic(errcp)
		}
		outC <- buf.String()
	}()
	var want string
	defer func() {
		w.Close()
		os.Stdout = stdout
		if out := <-outC; out != want {
			t.Errorf("logged HTTP request %s; want %s", out, want)
		}
	}()

	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")
	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}

	// see preceding deferred function, where want is used.
	want = fmt.Sprintf(golden(t.Name()), ts.URL, ts.Listener.Addr())
	testBody(t, resp.Body, []byte("Hello, world!"))
}

func outgoingGet(t *testing.T, client *http.Client, ts *httptest.Server, done func()) {
	defer done()
	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")
	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}
	testBody(t, resp.Body, []byte("Hello, world!"))
}

func TestOutgoingConcurrency(t *testing.T) {
	// don't run in parallel
	if race {
		t.Skip("cannot test because until data race issues are resolved on the standard library https://github.com/golang/go/issues/30597")
	}

	ts := httptest.NewServer(&helloHandler{})
	defer ts.Close()
	logger := &Logger{
		TLS:            true,
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	}
	logger.SetFlusher(OnEnd)
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	client := &http.Client{
		Transport: logger.RoundTripper(newTransport()),
	}

	var wg sync.WaitGroup
	concurrency := 100
	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go outgoingGet(t, client, ts, wg.Done)
		time.Sleep(time.Millisecond) // let's slow down just a little bit ("too many files descriptors open" on a slow machine, more realistic traffic, and so on)
	}
	wg.Wait()
	got := buf.String()
	if gotConcurrency := strings.Count(got, "< HTTP/1.1 200 OK"); concurrency != gotConcurrency {
		t.Errorf("logged %d requests, wanted %d", concurrency, gotConcurrency)
	}
	if want := fmt.Sprintf(golden(t.Name()), ts.URL, ts.Listener.Addr()); !strings.Contains(got, want) {
		t.Errorf("Request doesn't contain expected body")
	}
}

func TestOutgoingMinimal(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(&helloHandler{})
	defer ts.Close()

	// only prints the request URI.
	logger := &Logger{}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	client := &http.Client{
		Transport: logger.RoundTripper(newTransport()),
	}

	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}
	req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")
	req.AddCookie(&http.Cookie{
		Name:  "food",
		Value: "sorbet",
	})
	if _, err = client.Do(req); err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}
	if want, got := fmt.Sprintf("* Request to %s\n", ts.URL), buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

func TestOutgoingSanitized(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(&helloHandler{})
	defer ts.Close()

	logger := &Logger{
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	client := &http.Client{
		Transport: logger.RoundTripper(newTransport()),
	}

	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}
	req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")
	req.AddCookie(&http.Cookie{
		Name:  "food",
		Value: "sorbet",
	})
	if _, err = client.Do(req); err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}
	want := fmt.Sprintf(golden(t.Name()), ts.URL, ts.Listener.Addr())
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

func TestOutgoingSkipSanitize(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(&helloHandler{})
	defer ts.Close()

	logger := &Logger{
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
		SkipSanitize:   true,
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	client := &http.Client{
		Transport: logger.RoundTripper(newTransport()),
	}

	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}
	req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")
	req.AddCookie(&http.Cookie{
		Name:  "food",
		Value: "sorbet",
	})
	if _, err = client.Do(req); err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}
	want := fmt.Sprintf(golden(t.Name()), ts.URL, ts.Listener.Addr())
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

func TestOutgoingHide(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(&helloHandler{})
	defer ts.Close()

	logger := &Logger{
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	client := &http.Client{
		Transport: logger.RoundTripper(newTransport()),
	}

	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}
	req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")
	req = req.WithContext(WithHide(context.Background()))
	if _, err = client.Do(req); err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("request should not be logged, got %v", buf.String())
	}
	if got := buf.String(); got != "" {
		t.Errorf("logged HTTP request %s; want none", got)
	}
}

func filteredURIs(req *http.Request) (bool, error) {
	path := req.URL.Path
	if path == "/filtered" {
		return true, nil
	}
	if path == "/unfiltered" {
		return false, nil
	}
	return false, errors.New("filter error triggered")
}

func TestOutgoingFilter(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(&helloHandler{})
	defer ts.Close()

	logger := &Logger{
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	}
	logger.SetOutput(ioutil.Discard)
	logger.SetFilter(filteredURIs)
	client := &http.Client{
		Transport: logger.RoundTripper(newTransport()),
	}

	testCases := []struct {
		uri  string
		want string
	}{
		{uri: "filtered"},
		{uri: "unfiltered", want: "* Request"},
		{uri: "other", want: "filter error triggered"},
	}
	for _, tc := range testCases {
		t.Run(tc.uri, func(t *testing.T) {
			var buf bytes.Buffer
			logger.SetOutput(&buf)
			if _, err := client.Get(fmt.Sprintf("%s/%s", ts.URL, tc.uri)); err != nil {
				t.Errorf("cannot create request: %v", err)
			}
			if tc.want == "" && buf.Len() != 0 {
				t.Errorf("wanted input to be filtered, got %v instead", buf.String())
			}
			if !strings.Contains(buf.String(), tc.want) {
				t.Errorf(`expected input to contain "%v", got %v instead`, tc.want, buf.String())
			}
		})
	}
}

func TestOutgoingFilterPanicked(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(&helloHandler{})
	defer ts.Close()

	logger := &Logger{
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	}
	logger.SetOutput(ioutil.Discard)
	logger.SetFilter(func(req *http.Request) (bool, error) {
		panic("evil panic")
	})
	client := &http.Client{
		Transport: logger.RoundTripper(newTransport()),
	}

	var buf bytes.Buffer
	logger.SetOutput(&buf)
	if _, err := client.Get(ts.URL); err != nil {
		t.Errorf("cannot create request: %v", err)
	}
	want := fmt.Sprintf(golden(t.Name()), ts.URL, ts.URL, ts.Listener.Addr())
	if got := buf.String(); got != want {
		t.Errorf(`expected input to contain "%v", got %v instead`, want, got)
	}
}

func TestOutgoingSkipHeader(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(&jsonHandler{})
	defer ts.Close()

	logger := Logger{
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	}
	logger.SkipHeader([]string{
		"user-agent",
		"content-type",
	})
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	client := &http.Client{
		Transport: logger.RoundTripper(newTransport()),
	}

	uri := fmt.Sprintf("%s/json", ts.URL)
	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}
	req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")
	if _, err = client.Do(req); err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}
	want := fmt.Sprintf(golden(t.Name()), uri, ts.Listener.Addr())
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

func TestOutgoingBodyFilter(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(&jsonHandler{})
	defer ts.Close()

	logger := Logger{
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	}
	logger.SetBodyFilter(func(h http.Header) (skip bool, err error) {
		mediatype, _, _ := mime.ParseMediaType(h.Get("Content-Type"))
		return mediatype == "application/json", nil
	})
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	client := &http.Client{
		Transport: logger.RoundTripper(newTransport()),
	}

	uri := fmt.Sprintf("%s/json", ts.URL)
	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}
	req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")
	if _, err = client.Do(req); err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}
	want := fmt.Sprintf(golden(t.Name()), uri, ts.Listener.Addr())
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

func TestOutgoingBodyFilterSoftError(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(&jsonHandler{})
	defer ts.Close()

	logger := Logger{
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	}
	logger.SetBodyFilter(func(h http.Header) (skip bool, err error) {
		// filter anyway, but print soft error saying something went wrong during the filtering.
		return true, errors.New("incomplete implementation")
	})
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	client := &http.Client{
		Transport: logger.RoundTripper(newTransport()),
	}

	uri := fmt.Sprintf("%s/json", ts.URL)
	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}
	req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")
	if _, err = client.Do(req); err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}
	want := fmt.Sprintf(golden(t.Name()), uri, ts.Listener.Addr())
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

func TestOutgoingBodyFilterPanicked(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(&jsonHandler{})
	defer ts.Close()

	logger := Logger{
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	}
	logger.SetBodyFilter(func(h http.Header) (skip bool, err error) {
		panic("evil panic")
	})
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	client := &http.Client{
		Transport: logger.RoundTripper(newTransport()),
	}

	uri := fmt.Sprintf("%s/json", ts.URL)
	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}
	req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")
	if _, err = client.Do(req); err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}
	want := fmt.Sprintf(golden(t.Name()), uri, ts.Listener.Addr())
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

func TestOutgoingWithTimeRequest(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(&helloHandler{})
	defer ts.Close()

	logger := &Logger{
		Time: true,

		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	client := &http.Client{
		Transport: logger.RoundTripper(newTransport()),
	}

	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}
	req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")
	if _, err = client.Do(req); err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "* Request at ") {
		t.Error("missing printing start time of request")
	}
	if !strings.Contains(got, "* Request took ") {
		t.Error("missing printing request duration")
	}
}

type jsonHandler struct{}

func (h jsonHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header()["Date"] = nil
	if r.URL.Path == "/vnd" {
		w.Header().Set("Content-Type", "application/vnd.api+json")
	} else {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	}
	type res struct {
		Result string      `json:"result"`
		Number json.Number `json:"number"`
	}
	b, err := json.Marshal(res{
		Result: "Hello, world!",
		Number: json.Number("3.14"),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	fmt.Fprint(w, string(b))
}

func TestOutgoingFormattedJSON(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(&jsonHandler{})
	defer ts.Close()

	logger := Logger{
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
		Formatters: []Formatter{
			&JSONFormatter{},
		},
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	client := &http.Client{
		Transport: logger.RoundTripper(newTransport()),
	}

	testCases := []struct {
		name        string
		contentType string
	}{
		{
			name:        "json",
			contentType: "application/json",
		},
		{
			name:        "vnd",
			contentType: "application/vnd.api+json",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			buf.Reset()
			uri := fmt.Sprintf("%s/%s", ts.URL, tc.name)
			req, err := http.NewRequest(http.MethodGet, uri, nil)
			if err != nil {
				t.Errorf("cannot create request: %v", err)
			}
			req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")
			if _, err = client.Do(req); err != nil {
				t.Errorf("cannot connect to the server: %v", err)
			}
			want := fmt.Sprintf(golden(t.Name()), uri, ts.Listener.Addr())
			if got := buf.String(); got != want {
				t.Errorf("logged HTTP request %s; want %s", got, want)
			}
		})
	}
}

type badJSONHandler struct{}

func (h badJSONHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header()["Date"] = nil
	w.Header().Set("Content-Type", "application/json; charset=utf-8") // wrong content-type on purpose
	fmt.Fprint(w, `{"bad": }`)
}

func TestOutgoingBadJSON(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(&badJSONHandler{})
	defer ts.Close()

	logger := &Logger{
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
		Formatters: []Formatter{
			&JSONFormatter{},
		},
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	client := &http.Client{
		Transport: logger.RoundTripper(newTransport()),
	}

	uri := fmt.Sprintf("%s/json", ts.URL)
	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}
	req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")
	if _, err = client.Do(req); err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}
	want := fmt.Sprintf(golden(t.Name()), uri, ts.Listener.Addr())
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

type panickingFormatter struct{}

func (p *panickingFormatter) Match(mediatype string) bool {
	return true
}

func (p *panickingFormatter) Format(w io.Writer, src []byte) error {
	panic("evil formatter")
}

func TestOutgoingFormatterPanicked(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(&badJSONHandler{})
	defer ts.Close()

	logger := &Logger{
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
		Formatters: []Formatter{
			&panickingFormatter{},
		},
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	client := &http.Client{
		Transport: logger.RoundTripper(newTransport()),
	}

	uri := fmt.Sprintf("%s/json", ts.URL)
	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}
	req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")
	if _, err = client.Do(req); err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}
	want := fmt.Sprintf(golden(t.Name()), uri, ts.Listener.Addr())
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

type panickingFormatterMatcher struct{}

func (p *panickingFormatterMatcher) Match(mediatype string) bool {
	panic("evil matcher")
}

func (p *panickingFormatterMatcher) Format(w io.Writer, src []byte) error {
	return nil
}

func TestOutgoingFormatterMatcherPanicked(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(&badJSONHandler{})
	defer ts.Close()

	logger := &Logger{
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
		Formatters: []Formatter{
			&panickingFormatterMatcher{},
		},
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	client := &http.Client{
		Transport: logger.RoundTripper(newTransport()),
	}

	uri := fmt.Sprintf("%s/json", ts.URL)
	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}
	req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")
	if _, err = client.Do(req); err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}
	want := fmt.Sprintf(golden(t.Name()), uri, ts.Listener.Addr())
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

type formHandler struct{}

func (h formHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header()["Date"] = nil
	fmt.Fprint(w, "form received")
}

func TestOutgoingForm(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(&formHandler{})
	defer ts.Close()

	logger := &Logger{
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
		Formatters: []Formatter{
			&JSONFormatter{},
		},
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	client := &http.Client{
		Transport: logger.RoundTripper(newTransport()),
	}

	form := url.Values{}
	form.Add("foo", "bar")
	form.Add("email", "root@example.com")
	uri := fmt.Sprintf("%s/form", ts.URL)
	req, err := http.NewRequest(http.MethodPost, uri, strings.NewReader(form.Encode()))
	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}
	if _, err = client.Do(req); err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}
	want := fmt.Sprintf(golden(t.Name()), uri, ts.Listener.Addr())
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

func TestOutgoingBinaryBody(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header()["Date"] = nil
		fmt.Fprint(w, "\x25\x50\x44\x46\x2d\x31\x2e\x33\x0a\x25\xc4\xe5\xf2\xe5\xeb\xa7")
	}))
	defer ts.Close()

	logger := &Logger{
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	client := &http.Client{
		Transport: logger.RoundTripper(newTransport()),
	}

	b := []byte("RIFF\x00\x00\x00\x00WEBPVP")
	uri := fmt.Sprintf("%s/convert", ts.URL)
	req, err := http.NewRequest(http.MethodPost, uri, bytes.NewReader(b))
	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}
	req.Header.Add("Content-Type", "image/webp")
	if _, err = client.Do(req); err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}
	want := fmt.Sprintf(golden(t.Name()), uri, ts.Listener.Addr())
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

func TestOutgoingBinaryBodyNoMediatypeHeader(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header()["Date"] = nil
		w.Header()["Content-Type"] = nil
		fmt.Fprint(w, "\x25\x50\x44\x46\x2d\x31\x2e\x33\x0a\x25\xc4\xe5\xf2\xe5\xeb\xa7")
	}))
	defer ts.Close()

	logger := &Logger{
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	client := &http.Client{
		Transport: logger.RoundTripper(newTransport()),
	}

	b := []byte("RIFF\x00\x00\x00\x00WEBPVP")
	uri := fmt.Sprintf("%s/convert", ts.URL)
	req, err := http.NewRequest(http.MethodPost, uri, bytes.NewReader(b))
	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}

	if _, err = client.Do(req); err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}
	want := fmt.Sprintf(golden(t.Name()), uri, ts.Listener.Addr())
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

type longRequestHandler struct{}

func (h longRequestHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header()["Date"] = nil
	fmt.Fprint(w, "long request received")
}

func TestOutgoingLongRequest(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(&longRequestHandler{})
	defer ts.Close()

	logger := &Logger{
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
		Formatters: []Formatter{
			&JSONFormatter{},
		},
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	client := &http.Client{
		Transport: logger.RoundTripper(newTransport()),
	}

	uri := fmt.Sprintf("%s/long-request", ts.URL)
	req, err := http.NewRequest(http.MethodPut, uri, strings.NewReader(petition))
	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}
	if _, err = client.Do(req); err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}
	want := fmt.Sprintf(golden(t.Name()), uri, ts.Listener.Addr(), petition)
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

type longResponseHandler struct{}

func (h longResponseHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header()["Date"] = nil
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(petition)))
	if r.Method != http.MethodHead {
		fmt.Fprint(w, petition)
	}
}

func TestOutgoingLongResponse(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(&longResponseHandler{})
	defer ts.Close()

	logger := &Logger{
		RequestHeader:   true,
		RequestBody:     true,
		ResponseHeader:  true,
		ResponseBody:    true,
		MaxResponseBody: int64(len(petition) + 1000), // value larger than the text
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)

	client := &http.Client{
		Transport: logger.RoundTripper(newTransport()),
	}
	uri := fmt.Sprintf("%s/long-response", ts.URL)
	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}
	want := fmt.Sprintf(golden(t.Name()), uri, ts.Listener.Addr(), petition)
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
	testBody(t, resp.Body, []byte(petition))
}

func TestOutgoingLongResponseHead(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(&longResponseHandler{})
	defer ts.Close()
	logger := &Logger{
		RequestHeader:   true,
		RequestBody:     true,
		ResponseHeader:  true,
		ResponseBody:    true,
		MaxResponseBody: int64(len(petition) + 1000), // value larger than the text
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	client := &http.Client{
		Transport: logger.RoundTripper(newTransport()),
	}

	uri := fmt.Sprintf("%s/long-response", ts.URL)
	req, err := http.NewRequest(http.MethodHead, uri, nil)
	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}
	want := fmt.Sprintf(golden(t.Name()), uri, ts.Listener.Addr())
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
	testBody(t, resp.Body, []byte{})
}

func TestOutgoingTooLongResponse(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(&longResponseHandler{})
	defer ts.Close()
	logger := &Logger{
		RequestHeader:   true,
		RequestBody:     true,
		ResponseHeader:  true,
		ResponseBody:    true,
		MaxResponseBody: 5000, // value smaller than the text
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	client := &http.Client{
		Transport: logger.RoundTripper(newTransport()),
	}

	uri := fmt.Sprintf("%s/long-response", ts.URL)
	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}
	want := fmt.Sprintf(golden(t.Name()), uri, ts.Listener.Addr())
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
	testBody(t, resp.Body, []byte(petition))
}

type longResponseUnknownLengthHandler struct {
	repeat int
}

func (h longResponseUnknownLengthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header()["Date"] = nil
	fmt.Fprint(w, strings.Repeat(petition, h.repeat+1))
}

func TestOutgoingLongResponseUnknownLength(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name   string
		repeat int
	}{
		{name: "short", repeat: 1},
		{name: "long", repeat: 100},
	}

	want := golden(t.Name())
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(&longResponseUnknownLengthHandler{tc.repeat})
			defer ts.Close()
			logger := &Logger{
				RequestHeader:   true,
				RequestBody:     true,
				ResponseHeader:  true,
				ResponseBody:    true,
				MaxResponseBody: 10000000,
			}
			var buf bytes.Buffer
			logger.SetOutput(&buf)
			client := &http.Client{
				Transport: logger.RoundTripper(newTransport()),
			}

			uri := fmt.Sprintf("%s/long-response", ts.URL)
			req, err := http.NewRequest(http.MethodGet, uri, nil)
			if err != nil {
				t.Errorf("cannot create request: %v", err)
			}
			resp, err := client.Do(req)
			if err != nil {
				t.Errorf("cannot connect to the server: %v", err)
			}
			repeatedBody := strings.Repeat(petition, tc.repeat+1)
			want := fmt.Sprintf(want, uri, ts.Listener.Addr(), repeatedBody)
			if got := buf.String(); got != want {
				t.Errorf("logged HTTP request %s; want %s", got, want)
			}
			testBody(t, resp.Body, []byte(repeatedBody))
		})
	}
}

func TestOutgoingLongResponseUnknownLengthTooLong(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name   string
		repeat int
		max    int64
	}{
		{name: "short", repeat: 1, max: 4096},
		{name: "long", repeat: 100, max: 4096},
		{name: "long 1kb", repeat: 100, max: 1000},
	}

	want := golden(t.Name())
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(&longResponseUnknownLengthHandler{tc.repeat})
			defer ts.Close()
			logger := &Logger{
				RequestHeader:   true,
				RequestBody:     true,
				ResponseHeader:  true,
				ResponseBody:    true,
				MaxResponseBody: tc.max,
			}
			var buf bytes.Buffer
			logger.SetOutput(&buf)
			client := &http.Client{
				Transport: logger.RoundTripper(newTransport()),
			}

			uri := fmt.Sprintf("%s/long-response", ts.URL)
			req, err := http.NewRequest(http.MethodGet, uri, nil)
			if err != nil {
				t.Errorf("cannot create request: %v", err)
			}
			resp, err := client.Do(req)
			if err != nil {
				t.Errorf("cannot connect to the server: %v", err)
			}
			want := fmt.Sprintf(want, uri, ts.Listener.Addr())
			want = strings.Replace(want, "(contains more than 4096 bytes)", fmt.Sprintf("(contains more than %d bytes)", logger.MaxResponseBody), 1)
			if got := buf.String(); got != want {
				t.Errorf("logged HTTP request %s; want %s", got, want)
			}
			testBody(t, resp.Body, []byte(strings.Repeat(petition, tc.repeat+1)))
		})
	}
}

func multipartTestdata(writer *multipart.Writer, body *bytes.Buffer) {
	params := []struct {
		name  string
		value string
	}{
		{"author", "Frédéric Bastiat"},
		{"title", "Candlemakers' Petition"},
	}
	for _, p := range params {
		if err := writer.WriteField(p.name, p.value); err != nil {
			panic(err)
		}
	}

	part, err := writer.CreateFormFile("file", "petition")
	if err != nil {
		panic(err)
	}
	if _, err = part.Write([]byte(petition)); err != nil {
		panic(err)
	}
	if err = writer.Close(); err != nil {
		panic(err)
	}
}

type multipartHandler struct {
	t *testing.T
}

func (h multipartHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	t := h.t
	w.Header()["Date"] = nil
	if err := r.ParseMultipartForm(1000); err != nil {
		t.Errorf("cannot parse multipart form at server-side: %v", err)
	}
	if want, got := "Frédéric Bastiat", r.Form.Get("author"); want != got {
		t.Errorf("got author %s, wanted %s", got, want)
	}
	if want, got := "Candlemakers' Petition", r.Form.Get("title"); want != got {
		t.Errorf("got title %s, wanted %s", got, want)
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		t.Errorf("server cannot read file form sent over multipart: %v", err)
	}
	if want, got := "petition", header.Filename; want != got {
		t.Errorf("got filename %s, wanted %s", header.Filename, want)
	}
	if header.Size != int64(len(petition)) {
		t.Errorf("got size %d, wanted %d", header.Size, len(petition))
	}

	b, err := ioutil.ReadAll(file)
	if err != nil {
		t.Errorf("server cannot read file sent over multipart: %v", err)
	}
	if string(b) != petition {
		t.Error("server received different text than uploaded")
	}
	fmt.Fprint(w, "upload received")
}

func TestOutgoingMultipartForm(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(multipartHandler{t})
	defer ts.Close()

	logger := &Logger{
		RequestHeader: true,
		// TODO(henvic): print request body once support for printing out multipart/formdata body is added.
		ResponseHeader: true,
		ResponseBody:   true,
		Formatters: []Formatter{
			&JSONFormatter{},
		},
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	client := &http.Client{
		Transport: logger.RoundTripper(newTransport()),
	}

	uri := fmt.Sprintf("%s/multipart-upload", ts.URL)
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	multipartTestdata(writer, body)
	req, err := http.NewRequest(http.MethodPost, uri, body)
	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if _, err = client.Do(req); err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}
	want := fmt.Sprintf(golden(t.Name()), uri, ts.Listener.Addr(), writer.FormDataContentType())
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

func TestOutgoingTLS(t *testing.T) {
	t.Parallel()
	ts := httptest.NewTLSServer(&helloHandler{})
	defer ts.Close()

	logger := &Logger{
		TLS:            true,
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	client := ts.Client()
	client.Transport = logger.RoundTripper(client.Transport)

	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}
	req.Host = "example.com" // overriding the Host header to send
	req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")
	resp, err := client.Do(req)
	if err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}
	want := fmt.Sprintf(golden(t.Name()), ts.URL)
	if got := buf.String(); !regexp.MustCompile(want).MatchString(got) {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
	testBody(t, resp.Body, []byte("Hello, world!"))
}

func TestOutgoingTLSInsecureSkipVerify(t *testing.T) {
	t.Parallel()
	ts := httptest.NewTLSServer(&helloHandler{})
	defer ts.Close()

	logger := &Logger{
		TLS:            true,
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	client := ts.Client()
	transport := client.Transport.(*http.Transport)
	transport.TLSClientConfig.InsecureSkipVerify = true
	client.Transport = logger.RoundTripper(transport)

	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}
	req.Host = "example.com" // overriding the Host header to send
	req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")
	resp, err := client.Do(req)
	if err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}
	want := fmt.Sprintf(golden(t.Name()), ts.URL)
	if got := buf.String(); !regexp.MustCompile(want).MatchString(got) {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
	testBody(t, resp.Body, []byte("Hello, world!"))
}

func TestOutgoingTLSInvalidCertificate(t *testing.T) {
	t.Parallel()
	ts := httptest.NewTLSServer(&helloHandler{})
	ts.Config.ErrorLog = log.New(ioutil.Discard, "", 0)
	defer ts.Close()

	logger := &Logger{
		TLS:            true,
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	client := &http.Client{
		Transport: logger.RoundTripper(newTransport()),
	}

	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}
	req.Host = "example.com" // overriding the Host header to send
	req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")
	if _, err = client.Do(req); err == nil || !strings.Contains(err.Error(), "x509") {
		t.Errorf("cannot connect to the server has unexpected error: %v", err)
	}
	want := fmt.Sprintf(golden(t.Name()), ts.URL)
	if got := buf.String(); !regexp.MustCompile(want).MatchString(got) {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

func TestOutgoingTLSBadClientCertificate(t *testing.T) {
	t.Parallel()
	ts := httptest.NewUnstartedServer(&helloHandler{})
	ts.TLS = &tls.Config{
		ClientAuth: tls.RequireAndVerifyClientCert,
	}
	ts.StartTLS()
	defer ts.Close()

	logger := &Logger{
		TLS:            true,
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	ts.Config.ErrorLog = log.New(ioutil.Discard, "", 0)
	client := ts.Client()
	cert, err := tls.LoadX509KeyPair("testdata/cert-client.pem", "testdata/key-client.pem")
	if err != nil {
		panic(err)
	}
	cert.Leaf, err = x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Errorf("failed to parse certificate for copying Leaf field")
	}
	transport := client.Transport.(*http.Transport)
	transport.TLSClientConfig.Certificates = []tls.Certificate{
		cert,
	}
	client.Transport = logger.RoundTripper(transport)

	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}
	req.Host = "example.com" // overriding the Host header to send
	req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")
	if _, err = client.Do(req); err == nil || !strings.Contains(err.Error(), "bad certificate") {
		t.Errorf("got: %v, expected bad certificate error message", err)
	}
	want := fmt.Sprintf(golden(t.Name()), ts.URL)
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

func TestOutgoingHTTP2MutualTLS(t *testing.T) {
	t.Parallel()
	caCert, err := ioutil.ReadFile("testdata/cert.pem")
	if err != nil {
		panic(err)
	}
	clientCert, err := ioutil.ReadFile("testdata/cert-client.pem")
	if err != nil {
		panic(err)
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)
	caCertPool.AppendCertsFromPEM(clientCert)
	tlsConfig := &tls.Config{
		ClientCAs:  caCertPool,
		ClientAuth: tls.RequireAndVerifyClientCert,
	}

	// NOTE(henvic): Using httptest directly turned out complicated.
	// See https://venilnoronha.io/a-step-by-step-guide-to-mtls-in-go
	server := &http.Server{
		TLSConfig: tlsConfig,
		Handler:   &helloHandler{},
	}
	listener, err := netListener()
	if err != nil {
		panic(fmt.Sprintf("failed to listen on a port: %v", err))
	}
	defer listener.Close()
	go func() {
		// Certificate generated with
		// $ openssl req -x509 -newkey rsa:2048 \
		// -new -nodes -sha256 \
		// -days 36500 \
		// -out cert.pem \
		// -keyout key.pem \
		// -subj "/C=US/ST=California/L=Carmel-by-the-Sea/O=Plifk/OU=Cloud/CN=localhost" -extensions EXT -config <( \
		// printf "[dn]\nCN=localhost\n[req]\ndistinguished_name = dn\n[EXT]\nsubjectAltName=DNS:localhost\nkeyUsage=digitalSignature\nextendedKeyUsage=serverAuth, clientAuth")
		if errcp := server.ServeTLS(listener, "testdata/cert.pem", "testdata/key.pem"); errcp != http.ErrServerClosed {
			t.Errorf("server exit with unexpected error: %v", errcp)
		}
	}()
	defer server.Shutdown(context.Background())

	// Certificate generated with
	// $ openssl req -newkey rsa:2048 \
	// -new -nodes -x509 \
	// -days 36500 \
	// -out cert-client.pem \
	// -keyout key-client.pem \
	// -subj "/C=NL/ST=Zuid-Holland/L=Rotterdam/O=Client/OU=User/CN=User"
	cert, err := tls.LoadX509KeyPair("testdata/cert-client.pem", "testdata/key-client.pem")
	if err != nil {
		t.Errorf("failed to load X509 key pair: %v", err)
	}
	cert.Leaf, err = x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Errorf("failed to parse certificate for copying Leaf field")
	}

	// Create a HTTPS client and supply the created CA pool and certificate
	clientTLSConfig := &tls.Config{
		RootCAs:      caCertPool,
		Certificates: []tls.Certificate{cert},
	}
	transport := newTransport()
	transport.TLSClientConfig = clientTLSConfig
	client := &http.Client{
		Transport: transport,
	}
	logger := &Logger{
		TLS:            true,
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	client.Transport = logger.RoundTripper(client.Transport)
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		panic(err)
	}

	var host = fmt.Sprintf("https://localhost:%s/mutual-tls-test", port)
	resp, err := client.Get(host)
	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}
	testBody(t, resp.Body, []byte("Hello, world!"))
	want := fmt.Sprintf(golden(t.Name()), host, port)
	if got := buf.String(); !regexp.MustCompile(want).MatchString(got) {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

func TestOutgoingHTTP2MutualTLSNoSafetyLogging(t *testing.T) {
	t.Parallel()
	caCert, err := ioutil.ReadFile("testdata/cert.pem")
	if err != nil {
		panic(err)
	}
	clientCert, err := ioutil.ReadFile("testdata/cert-client.pem")
	if err != nil {
		panic(err)
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)
	caCertPool.AppendCertsFromPEM(clientCert)
	tlsConfig := &tls.Config{
		ClientCAs:  caCertPool,
		ClientAuth: tls.RequireAndVerifyClientCert,
	}

	// NOTE(henvic): Using httptest directly turned out complicated.
	// See https://venilnoronha.io/a-step-by-step-guide-to-mtls-in-go
	server := &http.Server{
		TLSConfig: tlsConfig,
		Handler:   &helloHandler{},
	}
	listener, err := netListener()
	if err != nil {
		panic(fmt.Sprintf("failed to listen on a port: %v", err))
	}
	defer listener.Close()
	go func() {
		// Certificate generated with
		// $ openssl req -x509 -newkey rsa:2048 \
		// -new -nodes -sha256 \
		// -days 36500 \
		// -out cert.pem \
		// -keyout key.pem \
		// -subj "/C=US/ST=California/L=Carmel-by-the-Sea/O=Plifk/OU=Cloud/CN=localhost" -extensions EXT -config <( \
		// printf "[dn]\nCN=localhost\n[req]\ndistinguished_name = dn\n[EXT]\nsubjectAltName=DNS:localhost\nkeyUsage=digitalSignature\nextendedKeyUsage=serverAuth, clientAuth")
		if errcp := server.ServeTLS(listener, "testdata/cert.pem", "testdata/key.pem"); errcp != http.ErrServerClosed {
			t.Errorf("server exit with unexpected error: %v", errcp)
		}
	}()
	defer server.Shutdown(context.Background())

	// Certificate generated with
	// $ openssl req -newkey rsa:2048 \
	// -new -nodes -x509 \
	// -days 36500 \
	// -out cert-client.pem \
	// -keyout key-client.pem \
	// -subj "/C=NL/ST=Zuid-Holland/L=Rotterdam/O=Client/OU=User/CN=User"
	cert, err := tls.LoadX509KeyPair("testdata/cert-client.pem", "testdata/key-client.pem")
	if err != nil {
		t.Errorf("failed to load X509 key pair: %v", err)
	}
	cert.Leaf, err = x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Errorf("failed to parse certificate for copying Leaf field")
	}

	// Create a HTTPS client and supply the created CA pool and certificate
	clientTLSConfig := &tls.Config{
		RootCAs:      caCertPool,
		Certificates: []tls.Certificate{cert},
	}
	transport := newTransport()
	transport.TLSClientConfig = clientTLSConfig
	client := &http.Client{
		Transport: transport,
	}
	logger := &Logger{
		// TLS must be false
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	client.Transport = logger.RoundTripper(client.Transport)
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		panic(err)
	}
	var host = fmt.Sprintf("https://localhost:%s/mutual-tls-test", port)
	resp, err := client.Get(host)
	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}
	testBody(t, resp.Body, []byte("Hello, world!"))
	want := fmt.Sprintf(golden(t.Name()), host, port)
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

// netListener is similar to httptest.newlocalListener() and listens locally in a random port.
// See https://github.com/golang/go/blob/5375c71289917ac7b25c6fa4bb0f4fa17be19a07/src/net/http/httptest/server.go#L60-L75
func netListener() (net.Listener, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return net.Listen("tcp6", "[::1]:0")
	}
	return listener, nil
}
