package httpretty

import (
	"bytes"
	_ "embed"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"testing"
	"time"
)

// race is a flag that can be usde to detect whether the race detector is on.
// It was added because as of Go 1.13.7 the TestOutgoingConcurrency test is failing because of a bug on the
// net/http standard library package.
// See race_test.go.
// See https://golang.org/issue/30597
var race bool

//go:embed testdata/petition.golden
// sample from http://bastiat.org/fr/petition.html
var petition string

func TestPrintRequest(t *testing.T) {
	t.Parallel()
	var req, err = http.NewRequest(http.MethodPost, "http://wxww.example.com/", nil)
	if err != nil {
		panic(err)
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
	logger.PrintRequest(req)

	want := `> POST / HTTP/1.1
> Host: wxww.example.com

`
	if got := buf.String(); got != want {
		t.Errorf("PrintRequest(req) = %v, wanted %v", got, want)
	}
}

func TestPrintRequestWithColors(t *testing.T) {
	t.Parallel()
	var req, err = http.NewRequest(http.MethodPost, "http://wxww.example.com/", nil)
	if err != nil {
		panic(err)
	}

	logger := &Logger{
		TLS:            true,
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
		Colors:         true,
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	logger.PrintRequest(req)
	want := "> \x1b[34;1mPOST\x1b[0m \x1b[33m/\x1b[0m \x1b[34mHTTP/1.1\x1b[0m" +
		"\n> \x1b[34;1mHost\x1b[0m\x1b[31m:\x1b[0m \x1b[33mwxww.example.com\x1b[0m\n\n"
	if got := buf.String(); got != want {
		t.Errorf("PrintRequest(req) = %v, wanted %v", got, want)
	}
}

func TestEncodingQueryStringParams(t *testing.T) {
	// Regression test for verifying query string parameters are being encoded correctly when printing with colors.
	// Issue reported by @mislav in https://github.com/henvic/httpretty/issues/9.
	t.Parallel()
	qs := url.Values{}
	qs.Set("a", "b")
	qs.Set("i", "j")
	qs.Set("x", "y")
	qs.Set("z", "+=")
	qs.Set("var", "foo&bar")
	u := url.URL{
		Scheme:   "http",
		Host:     "www.example.com",
		Path:     "/mypath",
		RawQuery: qs.Encode(),
	}
	var req, err = http.NewRequest(http.MethodPost, u.String(), nil)
	if err != nil {
		panic(err)
	}

	logger := &Logger{
		TLS:            true,
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
		Colors:         true,
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	logger.PrintRequest(req)
	want := "> \x1b[34;1mPOST\x1b[0m \x1b[33m/mypath?a=b&i=j&var=foo%26bar&x=y&z=%2B%3D\x1b[0m \x1b[34mHTTP/1.1\x1b[0m" +
		"\n> \x1b[34;1mHost\x1b[0m\x1b[31m:\x1b[0m \x1b[33mwww.example.com\x1b[0m\n\n"
	if got := buf.String(); got != want {
		t.Errorf("PrintRequest(req) = %v, wanted %v", got, want)
	}
}

func TestEncodingQueryStringParamsNoColors(t *testing.T) {
	t.Parallel()
	qs := url.Values{}
	qs.Set("a", "b")
	qs.Set("i", "j")
	qs.Set("x", "y")
	qs.Set("z", "+=")
	qs.Set("var", "foo&bar")
	u := url.URL{
		Scheme:   "http",
		Host:     "www.example.com",
		Path:     "/mypath",
		RawQuery: qs.Encode(),
	}
	var req, err = http.NewRequest(http.MethodPost, u.String(), nil)
	if err != nil {
		panic(err)
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
	logger.PrintRequest(req)
	want := `> POST /mypath?a=b&i=j&var=foo%26bar&x=y&z=%2B%3D HTTP/1.1
> Host: www.example.com

`
	if got := buf.String(); got != want {
		t.Errorf("PrintRequest(req) = %v, wanted %v", got, want)
	}
}

func TestPrintRequestFiltered(t *testing.T) {
	t.Parallel()
	var req, err = http.NewRequest(http.MethodPost, "http://wxww.example.com/", nil)
	if err != nil {
		panic(err)
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
	logger.SetFilter(func(req *http.Request) (skip bool, err error) {
		return true, nil
	})
	logger.PrintRequest(req)
	if got := buf.Len(); got != 0 {
		t.Errorf("got %v from logger, wanted nothing (everything should be filtered)", got)
	}
}

func TestPrintRequestNil(t *testing.T) {
	t.Parallel()
	logger := &Logger{
		TLS:            true,
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	}

	var buf bytes.Buffer
	logger.SetOutput(&buf)
	logger.PrintRequest(nil)
	want := "> error: null request\n"
	if got := buf.String(); got != want {
		t.Errorf("PrintRequest(req) = %v, wanted %v", got, want)
	}
}

func TestPrintResponseNil(t *testing.T) {
	t.Parallel()
	logger := &Logger{
		TLS:            true,
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	logger.PrintResponse(nil)

	want := "< error: null response\n"
	if got := buf.String(); got != want {
		t.Errorf("PrintResponse(req) = %v, wanted %v", got, want)
	}
}

func testBody(t *testing.T, r io.Reader, want []byte) {
	t.Helper()
	got, err := ioutil.ReadAll(r)
	if err != nil {
		t.Errorf("expected no error reading response body, got %v instead", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf(`got body = %v, wanted %v`, string(got), string(want))
	}
}

func TestJSONFormatterWriterError(t *testing.T) {
	// verifies if function doesn't panic if passed writer isn't *bytes.Buffer
	f := &JSONFormatter{}
	want := "underlying writer for JSONFormatter must be *bytes.Buffer"
	if err := f.Format(os.Stdout, []byte(`{}`)); err == nil || err.Error() != want {
		t.Errorf("got format error = %v, wanted %v", err, want)
	}
}

// newTransport creates a new HTTP Transport.
//
// BUG(henvic): this function is mostly used at this moment because of a data race condition on the standard library.
// See https://github.com/golang/go/issues/30597 for details.
func newTransport() *http.Transport {
	// values copied from Go 1.13.7 http.DefaultTransport variable.
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}
