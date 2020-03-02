package httpretty

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
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
	"strings"
	"sync"
	"testing"
	"time"
)

type helloHandler struct{}

func (h helloHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header()["Date"] = nil
	fmt.Fprintf(w, "Hello, world!")
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
		out := <-outC

		if out != want {
			t.Errorf("logged HTTP request %s; want %s", out, want)
		}
	}()

	client := &http.Client{
		// Only use the default transport (http.DefaultTransport) on TestOutgoing.
		// Passing nil here = http.DefaultTransport.
		Transport: logger.RoundTripper(nil),
	}

	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")

	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}

	resp, err := client.Do(req)

	if err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}

	want = fmt.Sprintf(`* Request to %s
> GET / HTTP/1.1
> Host: %s
> User-Agent: Robot/0.1 crawler@example.com

< HTTP/1.1 200 OK
< Content-Length: 13
< Content-Type: text/plain; charset=utf-8

Hello, world!
`, ts.URL, ts.Listener.Addr())

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

	if Race {
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

	gotConcurrency := strings.Count(got, "< HTTP/1.1 200 OK")

	if concurrency != gotConcurrency {
		t.Errorf("logged %d requests, wanted %d", concurrency, gotConcurrency)
	}

	want := fmt.Sprintf(`* Request to %s
> GET / HTTP/1.1
> Host: %s
> User-Agent: Robot/0.1 crawler@example.com

< HTTP/1.1 200 OK
< Content-Length: 13
< Content-Type: text/plain; charset=utf-8

Hello, world!`, ts.URL, ts.Listener.Addr())

	if !strings.Contains(got, want) {
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
	req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")

	req.AddCookie(&http.Cookie{
		Name:  "food",
		Value: "sorbet",
	})

	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}

	_, err = client.Do(req)

	if err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}

	want := fmt.Sprintf("* Request to %s\n", ts.URL)

	if got := buf.String(); got != want {
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
	req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")

	req.AddCookie(&http.Cookie{
		Name:  "food",
		Value: "sorbet",
	})

	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}

	_, err = client.Do(req)

	if err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}

	want := fmt.Sprintf(`* Request to %s
> GET / HTTP/1.1
> Host: %s
> Cookie: food=████████████████████
> User-Agent: Robot/0.1 crawler@example.com

< HTTP/1.1 200 OK
< Content-Length: 13
< Content-Type: text/plain; charset=utf-8

Hello, world!
`, ts.URL, ts.Listener.Addr())

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
	req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")

	req.AddCookie(&http.Cookie{
		Name:  "food",
		Value: "sorbet",
	})

	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}

	_, err = client.Do(req)

	if err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}

	want := fmt.Sprintf(`* Request to %s
> GET / HTTP/1.1
> Host: %s
> Cookie: food=sorbet
> User-Agent: Robot/0.1 crawler@example.com

< HTTP/1.1 200 OK
< Content-Length: 13
< Content-Type: text/plain; charset=utf-8

Hello, world!
`, ts.URL, ts.Listener.Addr())

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
	req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")

	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}

	req = req.WithContext(WithHide(context.Background()))

	_, err = client.Do(req)

	if err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}

	if buf.Len() != 0 {
		t.Errorf("request should not be logged, got %v", buf.String())
	}
	want := ""

	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
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

			_, err := client.Get(fmt.Sprintf("%s/%s", ts.URL, tc.uri))

			if err != nil {
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

	_, err := client.Get(ts.URL)

	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}

	want := fmt.Sprintf(`* cannot filter request: GET %v: panic: evil panic
* Request to %v
> GET / HTTP/1.1
> Host: %v

< HTTP/1.1 200 OK
< Content-Length: 13
< Content-Type: text/plain; charset=utf-8

Hello, world!
`, ts.URL, ts.URL, ts.Listener.Addr())

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
	req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")

	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}

	_, err = client.Do(req)

	if err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}

	want := fmt.Sprintf(`* Request to %s
> GET /json HTTP/1.1
> Host: %s

< HTTP/1.1 200 OK
< Content-Length: 40

{"result":"Hello, world!","number":3.14}
`, uri, ts.Listener.Addr())

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
	req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")

	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}

	_, err = client.Do(req)

	if err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}

	want := fmt.Sprintf(`* Request to %s
> GET /json HTTP/1.1
> Host: %s
> User-Agent: Robot/0.1 crawler@example.com

< HTTP/1.1 200 OK
< Content-Length: 40
< Content-Type: application/json; charset=utf-8

`, uri, ts.Listener.Addr())

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
	req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")

	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}

	_, err = client.Do(req)

	if err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}

	want := fmt.Sprintf(`* Request to %s
> GET /json HTTP/1.1
> Host: %s
> User-Agent: Robot/0.1 crawler@example.com

< HTTP/1.1 200 OK
< Content-Length: 40
< Content-Type: application/json; charset=utf-8

* error on response body filter: incomplete implementation
`, uri, ts.Listener.Addr())

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
	req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")

	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}

	_, err = client.Do(req)

	if err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}

	want := fmt.Sprintf(`* Request to %s
> GET /json HTTP/1.1
> Host: %s
> User-Agent: Robot/0.1 crawler@example.com

< HTTP/1.1 200 OK
< Content-Length: 40
< Content-Type: application/json; charset=utf-8

* panic while filtering body: evil panic
{"result":"Hello, world!","number":3.14}
`, uri, ts.Listener.Addr())

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
	req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")

	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}

	_, err = client.Do(req)

	if err != nil {
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
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

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
	}

	var buf bytes.Buffer
	logger.SetOutput(&buf)

	logger.Formatters = []Formatter{
		&JSONFormatter{},
	}

	client := &http.Client{
		Transport: logger.RoundTripper(newTransport()),
	}

	uri := fmt.Sprintf("%s/json", ts.URL)

	req, err := http.NewRequest(http.MethodGet, uri, nil)
	req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")

	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}

	_, err = client.Do(req)

	if err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}

	want := fmt.Sprintf(`* Request to %s
> GET /json HTTP/1.1
> Host: %s
> User-Agent: Robot/0.1 crawler@example.com

< HTTP/1.1 200 OK
< Content-Length: 40
< Content-Type: application/json; charset=utf-8

{
    "result": "Hello, world!",
    "number": 3.14
}
`, uri, ts.Listener.Addr())

	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
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
	}

	var buf bytes.Buffer
	logger.SetOutput(&buf)

	logger.Formatters = []Formatter{
		&JSONFormatter{},
	}

	client := &http.Client{
		Transport: logger.RoundTripper(newTransport()),
	}

	uri := fmt.Sprintf("%s/json", ts.URL)

	req, err := http.NewRequest(http.MethodGet, uri, nil)
	req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")

	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}

	_, err = client.Do(req)

	if err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}

	want := fmt.Sprintf(`* Request to %s
> GET /json HTTP/1.1
> Host: %s
> User-Agent: Robot/0.1 crawler@example.com

< HTTP/1.1 200 OK
< Content-Length: 9
< Content-Type: application/json; charset=utf-8

* body cannot be formatted: invalid character '}' looking for beginning of value
{"bad": }
`, uri, ts.Listener.Addr())

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
	}

	var buf bytes.Buffer
	logger.SetOutput(&buf)

	logger.Formatters = []Formatter{
		&panickingFormatter{},
	}

	client := &http.Client{
		Transport: logger.RoundTripper(newTransport()),
	}

	uri := fmt.Sprintf("%s/json", ts.URL)

	req, err := http.NewRequest(http.MethodGet, uri, nil)
	req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")

	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}

	_, err = client.Do(req)

	if err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}

	want := fmt.Sprintf(`* Request to %s
> GET /json HTTP/1.1
> Host: %s
> User-Agent: Robot/0.1 crawler@example.com

< HTTP/1.1 200 OK
< Content-Length: 9
< Content-Type: application/json; charset=utf-8

* body cannot be formatted: panic: evil formatter
{"bad": }
`, uri, ts.Listener.Addr())

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
	}

	var buf bytes.Buffer
	logger.SetOutput(&buf)

	logger.Formatters = []Formatter{
		&panickingFormatterMatcher{},
	}

	client := &http.Client{
		Transport: logger.RoundTripper(newTransport()),
	}

	uri := fmt.Sprintf("%s/json", ts.URL)

	req, err := http.NewRequest(http.MethodGet, uri, nil)
	req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")

	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}

	_, err = client.Do(req)

	if err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}

	want := fmt.Sprintf(`* Request to %s
> GET /json HTTP/1.1
> Host: %s
> User-Agent: Robot/0.1 crawler@example.com

< HTTP/1.1 200 OK
< Content-Length: 9
< Content-Type: application/json; charset=utf-8

* panic while testing body format: evil matcher
{"bad": }
`, uri, ts.Listener.Addr())

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
	}

	var buf bytes.Buffer
	logger.SetOutput(&buf)

	logger.Formatters = []Formatter{
		&JSONFormatter{},
	}

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

	_, err = client.Do(req)

	if err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}

	want := fmt.Sprintf(`* Request to %s
> POST /form HTTP/1.1
> Host: %s

email=root%%40example.com&foo=bar
< HTTP/1.1 200 OK
< Content-Length: 13
< Content-Type: text/plain; charset=utf-8

form received
`, uri, ts.Listener.Addr())

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
	}

	var buf bytes.Buffer
	logger.SetOutput(&buf)

	logger.Formatters = []Formatter{
		&JSONFormatter{},
	}

	client := &http.Client{
		Transport: logger.RoundTripper(newTransport()),
	}

	uri := fmt.Sprintf("%s/long-request", ts.URL)

	req, err := http.NewRequest(http.MethodPut, uri, strings.NewReader(petition))

	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}

	_, err = client.Do(req)

	if err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}

	want := fmt.Sprintf(`* Request to %s
> PUT /long-request HTTP/1.1
> Host: %s

%s
< HTTP/1.1 200 OK
< Content-Length: 21
< Content-Type: text/plain; charset=utf-8

long request received
`, uri, ts.Listener.Addr(), petition)

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
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	}

	var buf bytes.Buffer
	logger.SetOutput(&buf)

	logger.MaxResponseBody = int64(len(petition) + 1000) // value larger than the text

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

	want := fmt.Sprintf(`* Request to %s
> GET /long-response HTTP/1.1
> Host: %s

< HTTP/1.1 200 OK
< Content-Length: 9846
< Content-Type: text/plain; charset=utf-8

%s
`, uri, ts.Listener.Addr(), petition)

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
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	}

	var buf bytes.Buffer
	logger.SetOutput(&buf)

	logger.MaxResponseBody = int64(len(petition) + 1000) // value larger than the text

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

	want := fmt.Sprintf(`* Request to %s
> HEAD /long-response HTTP/1.1
> Host: %s

< HTTP/1.1 200 OK
< Content-Length: 9846

`, uri, ts.Listener.Addr())

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
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	}

	var buf bytes.Buffer
	logger.SetOutput(&buf)

	logger.MaxResponseBody = 5000 // value smaller than the text

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

	want := fmt.Sprintf(`* Request to %s
> GET /long-response HTTP/1.1
> Host: %s

< HTTP/1.1 200 OK
< Content-Length: 9846
< Content-Type: text/plain; charset=utf-8

* body is too long (9846 bytes) to print, skipping (longer than 5000 bytes)
`, uri, ts.Listener.Addr())

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

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(&longResponseUnknownLengthHandler{tc.repeat})
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

			uri := fmt.Sprintf("%s/long-response", ts.URL)

			req, err := http.NewRequest(http.MethodGet, uri, nil)

			if err != nil {
				t.Errorf("cannot create request: %v", err)
			}

			resp, err := client.Do(req)

			if err != nil {
				t.Errorf("cannot connect to the server: %v", err)
			}

			want := fmt.Sprintf(`* Request to %s
> GET /long-response HTTP/1.1
> Host: %s

< HTTP/1.1 200 OK
< Content-Type: text/plain; charset=utf-8

* body is too long, skipping (contains more than 4096 bytes)
`, uri, ts.Listener.Addr())

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

	wantAuthor := "Frédéric Bastiat"
	wantTitle := "Candlemakers' Petition"
	wantFilename := "petition"
	gotAuthor := r.Form.Get("author")
	gotTitle := r.Form.Get("title")

	if gotAuthor != wantAuthor {
		t.Errorf("got author %s, wanted %s", gotAuthor, wantAuthor)
	}

	if gotTitle != wantTitle {
		t.Errorf("got author %s, wanted %s", gotTitle, wantTitle)
	}

	file, header, err := r.FormFile("file")

	if err != nil {
		t.Errorf("server cannot read file form sent over multipart: %v", err)
	}

	if header.Filename != wantFilename {
		t.Errorf("got filename %s, wanted %s", header.Filename, wantFilename)
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
	}

	var buf bytes.Buffer
	logger.SetOutput(&buf)

	logger.Formatters = []Formatter{
		&JSONFormatter{},
	}

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

	_, err = client.Do(req)

	if err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}

	want := fmt.Sprintf(`* Request to %s
> POST /multipart-upload HTTP/1.1
> Host: %s
> Content-Type: %s

< HTTP/1.1 200 OK
< Content-Length: 15
< Content-Type: text/plain; charset=utf-8

upload received
`, uri, ts.Listener.Addr(), writer.FormDataContentType())

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

	req.Host = "example.com" // overriding the Host header to send

	req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")

	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}

	resp, err := client.Do(req)

	if err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}

	want := fmt.Sprintf(`* Request to %s
> GET / HTTP/1.1
> Host: example.com
> User-Agent: Robot/0.1 crawler@example.com

* TLS connection using TLS 1.3 / TLS_AES_128_GCM_SHA256
* Server certificate:
*  subject: O=Acme Co
*  start date: Thu Jan  1 00:00:00 UTC 1970
*  expire date: Sat Jan 29 16:00:00 UTC 2084
*  issuer: O=Acme Co
*  TLS certificate verify ok.
< HTTP/1.1 200 OK
< Content-Length: 13
< Content-Type: text/plain; charset=utf-8

Hello, world!
`, ts.URL)

	if got := buf.String(); got != want {
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

	req.Host = "example.com" // overriding the Host header to send

	req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")

	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}

	resp, err := client.Do(req)

	if err != nil {
		t.Errorf("cannot connect to the server: %v", err)
	}

	want := fmt.Sprintf(`* Request to %s
* Skipping TLS verification: connection is susceptible to man-in-the-middle attacks.
> GET / HTTP/1.1
> Host: example.com
> User-Agent: Robot/0.1 crawler@example.com

* TLS connection using TLS 1.3 / TLS_AES_128_GCM_SHA256 (insecure=true)
* Server certificate:
*  subject: O=Acme Co
*  start date: Thu Jan  1 00:00:00 UTC 1970
*  expire date: Sat Jan 29 16:00:00 UTC 2084
*  issuer: O=Acme Co
*  TLS certificate verify ok.
< HTTP/1.1 200 OK
< Content-Length: 13
< Content-Type: text/plain; charset=utf-8

Hello, world!
`, ts.URL)

	got := buf.String()

	if got != want {
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

	req.Host = "example.com" // overriding the Host header to send

	req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")

	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}

	_, err = client.Do(req)

	var unknownAuthorityError x509.UnknownAuthorityError
	if err == nil || !errors.As(err, &unknownAuthorityError) {
		t.Errorf("cannot connect to the server has unexpected error: %v", err)
	}

	want := fmt.Sprintf(`* Request to %s
> GET / HTTP/1.1
> Host: example.com
> User-Agent: Robot/0.1 crawler@example.com

* x509: certificate signed by unknown authority
`, ts.URL)

	if got := buf.String(); got != want {
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
	req.Host = "example.com" // overriding the Host header to send
	req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")

	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}

	_, err = client.Do(req)

	if err == nil || !strings.Contains(err.Error(), "bad certificate") {
		t.Errorf("got: %v, expected bad certificate error message", err)
	}

	want := fmt.Sprintf(`* Request to %s
* Client certificate:
*  subject: CN=User,OU=User,O=Client,L=Rotterdam,ST=Zuid-Holland,C=NL
*  start date: Sat Jan 25 20:12:36 UTC 2020
*  expire date: Mon Jan  1 20:12:36 UTC 2120
*  issuer: CN=User,OU=User,O=Client,L=Rotterdam,ST=Zuid-Holland,C=NL
> GET / HTTP/1.1
> Host: example.com
> User-Agent: Robot/0.1 crawler@example.com

* remote error: tls: bad certificate
`, ts.URL)

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
		// $ openssl req -newkey rsa:2048 \
		// -new -nodes -x509 \
		// -days 36500 \
		// -out cert.pem \
		// -keyout key.pem \
		// -subj "/C=US/ST=California/L=Carmel-by-the-Sea/O=Plifk/OU=Cloud/CN=localhost"
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

	want := fmt.Sprintf(`* Request to %s
* Client certificate:
*  subject: CN=User,OU=User,O=Client,L=Rotterdam,ST=Zuid-Holland,C=NL
*  start date: Sat Jan 25 20:12:36 UTC 2020
*  expire date: Mon Jan  1 20:12:36 UTC 2120
*  issuer: CN=User,OU=User,O=Client,L=Rotterdam,ST=Zuid-Holland,C=NL
> GET /mutual-tls-test HTTP/1.1
> Host: localhost:%s

* TLS connection using TLS 1.3 / TLS_AES_128_GCM_SHA256
* ALPN: h2 accepted
* Server certificate:
*  subject: CN=localhost,OU=Cloud,O=Plifk,L=Carmel-by-the-Sea,ST=California,C=US
*  start date: Sun Jan 19 18:14:57 UTC 2020
*  expire date: Tue Dec 26 18:14:57 UTC 2119
*  issuer: CN=localhost,OU=Cloud,O=Plifk,L=Carmel-by-the-Sea,ST=California,C=US
*  TLS certificate verify ok.
< HTTP/2.0 200 OK
< Content-Length: 13
< Content-Type: text/plain; charset=utf-8

Hello, world!
`, host, port)

	if got := buf.String(); got != want {
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
		// $ openssl req -newkey rsa:2048 \
		// -new -nodes -x509 \
		// -days 36500 \
		// -out cert.pem \
		// -keyout key.pem \
		// -subj "/C=US/ST=California/L=Carmel-by-the-Sea/O=Plifk/OU=Cloud/CN=localhost"
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

	want := fmt.Sprintf(`* Request to %s
> GET /mutual-tls-test HTTP/1.1
> Host: localhost:%s

< HTTP/2.0 200 OK
< Content-Length: 13
< Content-Type: text/plain; charset=utf-8

Hello, world!
`, host, port)

	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

// netListener is similar to httptest.newlocalListener() and listens locally in a random port.
// See https://github.com/golang/go/blob/5375c71289917ac7b25c6fa4bb0f4fa17be19a07/src/net/http/httptest/server.go#L60-L75
func netListener() (listener net.Listener, err error) {
	listener, err = net.Listen("tcp", "127.0.0.1:0")

	if err != nil {
		return net.Listen("tcp6", "[::1]:0")
	}

	return
}
