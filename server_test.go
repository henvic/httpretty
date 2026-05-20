package httpretty

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
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
)

// inspect a request (not concurrency safe).
func inspect(next http.Handler, wait int) *inspectHandler {
	is := &inspectHandler{
		next: next,
	}
	is.wg.Add(wait)
	return is
}

type inspectHandler struct {
	next http.Handler
	wg   sync.WaitGroup
	req  *http.Request
}

func (h *inspectHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	h.req = req
	h.next.ServeHTTP(w, req)
	h.wg.Done()
}

func (h *inspectHandler) Wait() {
	h.wg.Wait()
}

func TestIncoming(t *testing.T) {
	t.Parallel()
	logger := &Logger{
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	is := inspect(logger.Middleware(helloHandler{}), 1)
	ts := httptest.NewServer(is)
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}
	req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")
	go func() {
		client := newServerClient()
		resp, err := client.Do(req)
		if err != nil {
			t.Errorf("cannot connect to the server: %v", err)
		}
		testBody(t, resp.Body, []byte("Hello, world!"))
	}()

	is.Wait()
	want := fmt.Sprintf(golden(t.Name()), is.req.Host, is.req.RemoteAddr, ts.Listener.Addr())
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

func TestIncomingNotFound(t *testing.T) {
	t.Parallel()
	logger := &Logger{
		RequestHeader:  true,
		ResponseHeader: true,
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	is := inspect(logger.Middleware(http.NotFoundHandler()), 1)
	ts := httptest.NewServer(is)
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}
	req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")
	go func() {
		client := newServerClient()
		resp, err := client.Do(req)
		if err != nil {
			t.Errorf("cannot connect to the server: %v", err)
		}
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("got status code %v, wanted %v", resp.StatusCode, http.StatusNotFound)
		}
	}()
	is.Wait()
	want := fmt.Sprintf(golden(t.Name()), is.req.Host, is.req.RemoteAddr, ts.Listener.Addr())
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

func outgoingGetServer(client *http.Client, ts *httptest.Server, done func()) {
	defer done()
	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")
	if err != nil {
		panic(err)
	}
	if _, err := client.Do(req); err != nil {
		panic(err)
	}
}

func TestIncomingConcurrency(t *testing.T) {
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
	ts := httptest.NewServer(logger.Middleware(helloHandler{}))
	defer ts.Close()

	concurrency := 100
	{
		var wg sync.WaitGroup
		wg.Add(concurrency)
		i := 0
	repeat:
		client := &http.Client{
			Transport: newTransport(),
		}
		go outgoingGetServer(client, ts, wg.Done)
		if i < concurrency-1 {
			i++
			time.Sleep(2 * time.Millisecond)
			goto repeat
		}
		wg.Wait()
	}

	got := buf.String()
	gotConcurrency := strings.Count(got, "< HTTP/1.1 200 OK")
	if concurrency != gotConcurrency {
		t.Errorf("logged %d requests, wanted %d", concurrency, gotConcurrency)
	}
	want := fmt.Sprintf(golden(t.Name()), ts.Listener.Addr())
	if !strings.Contains(got, want) {
		t.Errorf("Request doesn't contain expected body")
	}
}

func TestIncomingMinimal(t *testing.T) {
	t.Parallel()
	// only prints the request URI and remote address that requested it.
	logger := &Logger{}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	is := inspect(logger.Middleware(helloHandler{}), 1)

	ts := httptest.NewServer(is)
	defer ts.Close()
	uri := fmt.Sprintf("%s/incoming", ts.URL)
	go func() {
		client := newServerClient()
		req, err := http.NewRequest(http.MethodGet, uri, nil)
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
	}()
	is.Wait()
	want := fmt.Sprintf(golden(t.Name()), uri, is.req.RemoteAddr)
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

func TestIncomingSanitized(t *testing.T) {
	t.Parallel()
	logger := &Logger{
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	is := inspect(logger.Middleware(helloHandler{}), 1)

	ts := httptest.NewServer(is)
	defer ts.Close()
	uri := fmt.Sprintf("%s/incoming", ts.URL)
	go func() {
		client := newServerClient()
		req, err := http.NewRequest(http.MethodGet, uri, nil)
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
	}()
	is.Wait()
	want := fmt.Sprintf(golden(t.Name()), uri, is.req.RemoteAddr, ts.Listener.Addr())
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

type hideHandler struct {
	next http.Handler
}

func (h hideHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	req = req.WithContext(WithHide(context.Background()))
	h.next.ServeHTTP(w, req)
}

func TestIncomingHide(t *testing.T) {
	t.Parallel()
	logger := &Logger{
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	is := inspect(hideHandler{
		next: logger.Middleware(helloHandler{}),
	}, 1)
	ts := httptest.NewServer(is)
	defer ts.Close()
	go func() {
		client := newServerClient()
		req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
		req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")
		if err != nil {
			t.Errorf("cannot create request: %v", err)
		}
		if _, err = client.Do(req); err != nil {
			t.Errorf("cannot connect to the server: %v", err)
		}
	}()
	is.Wait()
	if buf.Len() != 0 {
		t.Errorf("request should not be logged, got %v", buf.String())
	}
}

func TestIncomingFilter(t *testing.T) {
	t.Parallel()
	logger := &Logger{
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	logger.SetFilter(filteredURIs)
	ts := httptest.NewServer(logger.Middleware(helloHandler{}))
	defer ts.Close()
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
			client := newServerClient()
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

func TestIncomingFilterPanicked(t *testing.T) {
	t.Parallel()
	logger := &Logger{
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	logger.SetFilter(func(req *http.Request) (bool, error) {
		panic("evil panic")
	})
	is := inspect(logger.Middleware(helloHandler{}), 1)
	ts := httptest.NewServer(is)
	defer ts.Close()
	client := newServerClient()
	_, err := client.Get(ts.URL)
	if err != nil {
		t.Errorf("cannot create request: %v", err)
	}
	want := fmt.Sprintf(golden(t.Name()), ts.URL, is.req.RemoteAddr, ts.Listener.Addr())
	if got := buf.String(); got != want {
		t.Errorf(`expected input to contain "%v", got %v instead`, want, got)
	}
}

func TestIncomingSkipHeader(t *testing.T) {
	t.Parallel()
	logger := &Logger{
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	logger.SkipHeader([]string{
		"user-agent",
		"content-type",
	})
	is := inspect(logger.Middleware(jsonHandler{}), 1)
	ts := httptest.NewServer(is)
	defer ts.Close()
	client := newServerClient()
	uri := fmt.Sprintf("%s/json", ts.URL)
	go func() {
		req, err := http.NewRequest(http.MethodGet, uri, nil)
		if err != nil {
			t.Errorf("cannot create request: %v", err)
		}
		req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")
		if _, err = client.Do(req); err != nil {
			t.Errorf("cannot connect to the server: %v", err)
		}
	}()
	is.Wait()
	want := fmt.Sprintf(golden(t.Name()), uri, is.req.RemoteAddr, ts.Listener.Addr())
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

func TestIncomingBodyFilter(t *testing.T) {
	t.Parallel()
	logger := &Logger{
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	logger.SetBodyFilter(func(h http.Header) (skip bool, err error) {
		mediatype, _, _ := mime.ParseMediaType(h.Get("Content-Type"))
		return mediatype == "application/json", nil
	})
	is := inspect(logger.Middleware(jsonHandler{}), 1)

	ts := httptest.NewServer(is)
	defer ts.Close()
	client := newServerClient()
	uri := fmt.Sprintf("%s/json", ts.URL)
	go func() {
		req, err := http.NewRequest(http.MethodGet, uri, nil)
		if err != nil {
			t.Errorf("cannot create request: %v", err)
		}
		req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")
		if _, err = client.Do(req); err != nil {
			t.Errorf("cannot connect to the server: %v", err)
		}
	}()
	is.Wait()
	want := fmt.Sprintf(golden(t.Name()), uri, is.req.RemoteAddr, ts.Listener.Addr())
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

func TestIncomingBodyFilterSoftError(t *testing.T) {
	t.Parallel()
	logger := &Logger{
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	logger.SetBodyFilter(func(h http.Header) (skip bool, err error) {
		// filter anyway, but print soft error saying something went wrong during the filtering.
		return true, errors.New("incomplete implementation")
	})
	is := inspect(logger.Middleware(jsonHandler{}), 1)

	ts := httptest.NewServer(is)
	defer ts.Close()
	client := newServerClient()
	uri := fmt.Sprintf("%s/json", ts.URL)
	go func() {
		req, err := http.NewRequest(http.MethodGet, uri, nil)
		if err != nil {
			t.Errorf("cannot create request: %v", err)
		}
		req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")
		if _, err = client.Do(req); err != nil {
			t.Errorf("cannot connect to the server: %v", err)
		}
	}()
	is.Wait()
	want := fmt.Sprintf(golden(t.Name()), uri, is.req.RemoteAddr, ts.Listener.Addr())
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

func TestIncomingBodyFilterPanicked(t *testing.T) {
	t.Parallel()
	logger := &Logger{
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	logger.SetBodyFilter(func(h http.Header) (skip bool, err error) {
		panic("evil panic")
	})
	is := inspect(logger.Middleware(jsonHandler{}), 1)
	ts := httptest.NewServer(is)
	defer ts.Close()

	client := newServerClient()
	uri := fmt.Sprintf("%s/json", ts.URL)
	go func() {
		req, err := http.NewRequest(http.MethodGet, uri, nil)
		if err != nil {
			t.Errorf("cannot create request: %v", err)
		}
		req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")
		if _, err = client.Do(req); err != nil {
			t.Errorf("cannot connect to the server: %v", err)
		}
	}()
	is.Wait()
	want := fmt.Sprintf(golden(t.Name()), uri, is.req.RemoteAddr, ts.Listener.Addr())
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

func TestIncomingWithTimeRequest(t *testing.T) {
	t.Parallel()
	logger := &Logger{
		Time:           true,
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)

	is := inspect(logger.Middleware(helloHandler{}), 1)
	ts := httptest.NewServer(is)
	defer ts.Close()
	go func() {
		client := &http.Client{
			Transport: newTransport(),
		}
		req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
		if err != nil {
			t.Errorf("cannot create request: %v", err)
		}
		req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")
		if _, err = client.Do(req); err != nil {
			t.Errorf("cannot connect to the server: %v", err)
		}
	}()
	is.Wait()
	got := buf.String()
	if !strings.Contains(got, "* Request at ") {
		t.Error("missing printing start time of request")
	}
	if !strings.Contains(got, "* Request took ") {
		t.Error("missing printing request duration")
	}
}

func TestIncomingFormattedJSON(t *testing.T) {
	t.Parallel()
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
	is := inspect(logger.Middleware(jsonHandler{}), 1)

	ts := httptest.NewServer(is)
	defer ts.Close()
	client := newServerClient()
	uri := fmt.Sprintf("%s/json", ts.URL)
	go func() {
		req, err := http.NewRequest(http.MethodGet, uri, nil)
		if err != nil {
			t.Errorf("cannot create request: %v", err)
		}
		req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")
		if _, err = client.Do(req); err != nil {
			t.Errorf("cannot connect to the server: %v", err)
		}
	}()
	is.Wait()
	want := fmt.Sprintf(golden(t.Name()), uri, is.req.RemoteAddr, ts.Listener.Addr())
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

func TestIncomingBadJSON(t *testing.T) {
	t.Parallel()
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
	is := inspect(logger.Middleware(badJSONHandler{}), 1)

	ts := httptest.NewServer(is)
	defer ts.Close()
	uri := fmt.Sprintf("%s/json", ts.URL)
	go func() {
		client := newServerClient()
		req, err := http.NewRequest(http.MethodGet, uri, nil)
		if err != nil {
			t.Errorf("cannot create request: %v", err)
		}
		req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")
		if _, err = client.Do(req); err != nil {
			t.Errorf("cannot connect to the server: %v", err)
		}
	}()
	is.Wait()
	want := fmt.Sprintf(golden(t.Name()), uri, is.req.RemoteAddr, ts.Listener.Addr())
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

func TestIncomingFormatterPanicked(t *testing.T) {
	t.Parallel()
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
	is := inspect(logger.Middleware(badJSONHandler{}), 1)

	ts := httptest.NewServer(is)
	defer ts.Close()
	uri := fmt.Sprintf("%s/json", ts.URL)
	go func() {
		client := newServerClient()
		req, err := http.NewRequest(http.MethodGet, uri, nil)
		if err != nil {
			t.Errorf("cannot create request: %v", err)
		}
		req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")
		if _, err = client.Do(req); err != nil {
			t.Errorf("cannot connect to the server: %v", err)
		}
	}()
	is.Wait()
	want := fmt.Sprintf(golden(t.Name()), uri, is.req.RemoteAddr, ts.Listener.Addr())
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

func TestIncomingFormatterMatcherPanicked(t *testing.T) {
	t.Parallel()
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
	is := inspect(logger.Middleware(badJSONHandler{}), 1)

	ts := httptest.NewServer(is)
	defer ts.Close()
	uri := fmt.Sprintf("%s/json", ts.URL)
	go func() {
		client := newServerClient()
		req, err := http.NewRequest(http.MethodGet, uri, nil)
		if err != nil {
			t.Errorf("cannot create request: %v", err)
		}
		req.Header.Add("User-Agent", "Robot/0.1 crawler@example.com")
		if _, err = client.Do(req); err != nil {
			t.Errorf("cannot connect to the server: %v", err)
		}
	}()
	is.Wait()
	want := fmt.Sprintf(golden(t.Name()), uri, is.req.RemoteAddr, ts.Listener.Addr())

	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

func TestIncomingForm(t *testing.T) {
	t.Parallel()
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
	is := inspect(logger.Middleware(formHandler{}), 1)

	ts := httptest.NewServer(is)
	defer ts.Close()
	uri := fmt.Sprintf("%s/form", ts.URL)
	go func() {
		client := newServerClient()
		form := url.Values{}
		form.Add("foo", "bar")
		form.Add("email", "root@example.com")
		req, err := http.NewRequest(http.MethodPost, uri, strings.NewReader(form.Encode()))
		if err != nil {
			t.Errorf("cannot create request: %v", err)
		}
		if _, err = client.Do(req); err != nil {
			t.Errorf("cannot connect to the server: %v", err)
		}
	}()
	is.Wait()
	want := fmt.Sprintf(golden(t.Name()), uri, is.req.RemoteAddr, ts.Listener.Addr())
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

func TestIncomingBinaryBody(t *testing.T) {
	t.Parallel()
	logger := &Logger{
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	is := inspect(logger.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header()["Date"] = nil
		fmt.Fprint(w, "\x25\x50\x44\x46\x2d\x31\x2e\x33\x0a\x25\xc4\xe5\xf2\xe5\xeb\xa7")
	})), 1)

	ts := httptest.NewServer(is)
	defer ts.Close()
	uri := fmt.Sprintf("%s/convert", ts.URL)
	go func() {
		client := newServerClient()
		b := []byte("RIFF\x00\x00\x00\x00WEBPVP")
		req, err := http.NewRequest(http.MethodPost, uri, bytes.NewReader(b))
		if err != nil {
			t.Errorf("cannot create request: %v", err)
		}
		req.Header.Add("Content-Type", "image/webp")
		if _, err = client.Do(req); err != nil {
			t.Errorf("cannot connect to the server: %v", err)
		}
	}()
	is.Wait()
	want := fmt.Sprintf(golden(t.Name()), uri, is.req.RemoteAddr, ts.Listener.Addr())
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

func TestIncomingBinaryBodyNoMediatypeHeader(t *testing.T) {
	t.Parallel()
	logger := &Logger{
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	is := inspect(logger.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header()["Date"] = nil
		w.Header()["Content-Type"] = nil
		fmt.Fprint(w, "\x25\x50\x44\x46\x2d\x31\x2e\x33\x0a\x25\xc4\xe5\xf2\xe5\xeb\xa7")
	})), 1)

	ts := httptest.NewServer(is)
	defer ts.Close()
	uri := fmt.Sprintf("%s/convert", ts.URL)
	go func() {
		client := newServerClient()
		b := []byte("RIFF\x00\x00\x00\x00WEBPVP")
		req, err := http.NewRequest(http.MethodPost, uri, bytes.NewReader(b))
		if err != nil {
			t.Errorf("cannot create request: %v", err)
		}
		if _, err = client.Do(req); err != nil {
			t.Errorf("cannot connect to the server: %v", err)
		}
	}()
	is.Wait()
	want := fmt.Sprintf(golden(t.Name()), uri, is.req.RemoteAddr, ts.Listener.Addr())
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

func TestIncomingBinaryResponseTextRequest(t *testing.T) {
	t.Parallel()
	logger := &Logger{
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	is := inspect(logger.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header()["Date"] = nil
		// Respond with a binary Content-Type but a body that has no binary bytes,
		// so only the Content-Type header check can catch it (not the byte-level heuristic).
		w.Header().Set("Content-Type", "application/pdf")
		fmt.Fprint(w, "not really a pdf")
	})), 1)

	ts := httptest.NewServer(is)
	defer ts.Close()
	go func() {
		client := newServerClient()
		req, err := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(`{"query":"convert"}`))
		if err != nil {
			t.Errorf("cannot create request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if _, err = client.Do(req); err != nil {
			t.Errorf("cannot connect to the server: %v", err)
		}
	}()
	is.Wait()
	got := buf.String()
	// The response body should be detected as binary based on the response Content-Type (application/pdf),
	// not the request Content-Type (application/json).
	if !strings.Contains(got, "* body contains binary data") {
		t.Errorf("expected response body to be detected as binary based on response Content-Type, got:\n%s", got)
	}
	if !strings.Contains(got, `{"query":"convert"}`) {
		t.Errorf("expected request body to be printed, but it was missing from:\n%s", got)
	}
}

func TestIncomingFlusher(t *testing.T) {
	t.Parallel()
	logger := &Logger{
		ResponseHeader: true,
		ResponseBody:   true,
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	is := inspect(logger.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header()["Date"] = nil
		w.Header().Set("Content-Type", "text/plain")
		// Flush must not panic when the middleware wraps the ResponseWriter.
		if f, ok := w.(http.Flusher); ok {
			fmt.Fprint(w, "streamed")
			f.Flush()
		} else {
			t.Error("expected ResponseWriter to implement http.Flusher")
		}
	})), 1)

	ts := httptest.NewServer(is)
	defer ts.Close()
	go func() {
		client := newServerClient()
		resp, err := client.Get(ts.URL)
		if err != nil {
			t.Errorf("cannot connect to the server: %v", err)
		}
		defer resp.Body.Close()
		testBody(t, resp.Body, []byte("streamed"))
	}()
	is.Wait()
	got := buf.String()
	if !strings.Contains(got, "200 OK") {
		t.Errorf("expected 200 OK in output, got:\n%s", got)
	}
	if !strings.Contains(got, "streamed") {
		t.Errorf("expected streamed body in output, got:\n%s", got)
	}
}

// plainWriter is a minimal http.ResponseWriter that does not implement
// http.Flusher, used to verify the middleware does not falsely advertise
// flushing when the underlying writer cannot.
type plainWriter struct {
	h          http.Header
	statusCode int
	body       bytes.Buffer
}

func (p *plainWriter) Header() http.Header {
	if p.h == nil {
		p.h = http.Header{}
	}
	return p.h
}
func (p *plainWriter) Write(b []byte) (int, error) { return p.body.Write(b) }
func (p *plainWriter) WriteHeader(c int)           { p.statusCode = c }

func TestIncomingNonFlushableUnderlying(t *testing.T) {
	t.Parallel()
	logger := &Logger{
		ResponseHeader: true,
		ResponseBody:   true,
	}
	var logBuf bytes.Buffer
	logger.SetOutput(&logBuf)

	var sawFlusher bool
	var rcFlushErr error
	handler := logger.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, sawFlusher = w.(http.Flusher)
		rcFlushErr = http.NewResponseController(w).Flush()
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "ok")
	}))

	pw := &plainWriter{}
	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	handler.ServeHTTP(pw, req)

	if sawFlusher {
		t.Error("inner handler should not see http.Flusher when underlying is not flushable")
	}
	if rcFlushErr == nil {
		t.Error("expected http.NewResponseController(w).Flush() to return an error")
	}
	// A non-flushable writer must not stop the response: it still reaches the
	// client and is recorded by the middleware.
	if got := pw.body.String(); got != "ok" {
		t.Errorf("underlying writer body = %q, want %q", got, "ok")
	}
	if got := logBuf.String(); !strings.Contains(got, "ok") {
		t.Errorf("expected recorded body in log output, got:\n%s", got)
	}
}

func TestIncomingResponseController(t *testing.T) {
	t.Parallel()
	logger := &Logger{
		ResponseHeader: true,
		ResponseBody:   true,
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	is := inspect(logger.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header()["Date"] = nil
		w.Header().Set("Content-Type", "text/plain")
		rc := http.NewResponseController(w)
		fmt.Fprint(w, "streamed")
		if err := rc.Flush(); err != nil {
			t.Errorf("rc.Flush(): %v", err)
		}
		// SetReadDeadline is not on the wrapper; the controller must walk
		// Unwrap to reach it on the underlying writer.
		if err := rc.SetReadDeadline(time.Time{}); err != nil {
			t.Errorf("rc.SetReadDeadline(): %v", err)
		}
	})), 1)

	ts := httptest.NewServer(is)
	defer ts.Close()
	go func() {
		client := newServerClient()
		resp, err := client.Get(ts.URL)
		if err != nil {
			t.Errorf("cannot connect to the server: %v", err)
		}
		defer resp.Body.Close()
		testBody(t, resp.Body, []byte("streamed"))
	}()
	is.Wait()
}

func TestIncomingLongRequest(t *testing.T) {
	t.Parallel()
	logger := &Logger{
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	is := inspect(logger.Middleware(longRequestHandler{}), 1)

	ts := httptest.NewServer(is)
	defer ts.Close()
	uri := fmt.Sprintf("%s/long-request", ts.URL)
	go func() {
		client := newServerClient()
		req, err := http.NewRequest(http.MethodPut, uri, strings.NewReader(petition))
		if err != nil {
			t.Errorf("cannot create request: %v", err)
		}
		if _, err = client.Do(req); err != nil {
			t.Errorf("cannot connect to the server: %v", err)
		}
	}()
	is.Wait()
	want := fmt.Sprintf(golden(t.Name()), uri, is.req.RemoteAddr, ts.Listener.Addr(), petition)
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

func TestIncomingLongResponse(t *testing.T) {
	t.Parallel()
	logger := &Logger{
		RequestHeader:   true,
		RequestBody:     true,
		ResponseHeader:  true,
		ResponseBody:    true,
		MaxResponseBody: int64(len(petition) + 1000), // value larger than the text
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	is := inspect(logger.Middleware(longResponseHandler{}), 1)

	ts := httptest.NewServer(is)
	defer ts.Close()
	uri := fmt.Sprintf("%s/long-response", ts.URL)
	go func() {
		client := newServerClient()
		req, err := http.NewRequest(http.MethodGet, uri, nil)
		if err != nil {
			t.Errorf("cannot create request: %v", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Errorf("cannot connect to the server: %v", err)
		}
		testBody(t, resp.Body, []byte(petition))
	}()
	is.Wait()
	want := fmt.Sprintf(golden(t.Name()), uri, is.req.RemoteAddr, ts.Listener.Addr(), petition)
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

func TestIncomingLongResponseHead(t *testing.T) {
	t.Parallel()
	logger := &Logger{
		RequestHeader:   true,
		RequestBody:     true,
		ResponseHeader:  true,
		ResponseBody:    true,
		MaxResponseBody: int64(len(petition) + 1000), // value larger than the text
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	is := inspect(logger.Middleware(longResponseHandler{}), 1)

	ts := httptest.NewServer(is)
	defer ts.Close()
	client := newServerClient()
	uri := fmt.Sprintf("%s/long-response", ts.URL)
	go func() {
		req, err := http.NewRequest(http.MethodHead, uri, nil)
		if err != nil {
			t.Errorf("cannot create request: %v", err)
		}
		if _, err = client.Do(req); err != nil {
			t.Errorf("cannot connect to the server: %v", err)
		}
	}()
	is.Wait()
	want := fmt.Sprintf(golden(t.Name()), uri, is.req.RemoteAddr, ts.Listener.Addr())
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

func TestIncomingTooLongResponse(t *testing.T) {
	t.Parallel()
	logger := &Logger{
		RequestHeader:   true,
		RequestBody:     true,
		ResponseHeader:  true,
		ResponseBody:    true,
		MaxResponseBody: 5000, // value smaller than the text
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	is := inspect(logger.Middleware(longResponseHandler{}), 1)

	ts := httptest.NewServer(is)
	defer ts.Close()
	uri := fmt.Sprintf("%s/long-response", ts.URL)
	go func() {
		client := newServerClient()
		req, err := http.NewRequest(http.MethodGet, uri, nil)
		if err != nil {
			t.Errorf("cannot create request: %v", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Errorf("cannot connect to the server: %v", err)
		}
		testBody(t, resp.Body, []byte(petition))
	}()
	is.Wait()
	want := fmt.Sprintf(golden(t.Name()), uri, is.req.RemoteAddr, ts.Listener.Addr())
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

func TestIncomingLongResponseUnknownLength(t *testing.T) {
	t.Parallel()
	logger := &Logger{
		RequestHeader:   true,
		RequestBody:     true,
		ResponseHeader:  true,
		ResponseBody:    true,
		MaxResponseBody: 10000000,
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)

	repeat := 100
	is := inspect(logger.Middleware(longResponseUnknownLengthHandler{repeat: repeat}), 1)
	ts := httptest.NewServer(is)
	defer ts.Close()
	uri := fmt.Sprintf("%s/long-response", ts.URL)
	repeatedBody := strings.Repeat(petition, repeat+1)
	go func() {
		client := newServerClient()
		req, err := http.NewRequest(http.MethodGet, uri, nil)
		if err != nil {
			t.Errorf("cannot create request: %v", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Errorf("cannot connect to the server: %v", err)
		}
		testBody(t, resp.Body, []byte(repeatedBody))
	}()
	is.Wait()
	want := fmt.Sprintf(golden(t.Name()), uri, is.req.RemoteAddr, ts.Listener.Addr(), repeatedBody)
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

func TestIncomingLongResponseUnknownLengthTooLong(t *testing.T) {
	t.Parallel()
	logger := &Logger{
		RequestHeader:   true,
		RequestBody:     true,
		ResponseHeader:  true,
		ResponseBody:    true,
		MaxResponseBody: 5000, // value smaller than the text
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	is := inspect(logger.Middleware(longResponseUnknownLengthHandler{}), 1)

	ts := httptest.NewServer(is)
	defer ts.Close()
	uri := fmt.Sprintf("%s/long-response", ts.URL)
	go func() {
		client := newServerClient()
		req, err := http.NewRequest(http.MethodGet, uri, nil)
		if err != nil {
			t.Errorf("cannot create request: %v", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Errorf("cannot connect to the server: %v", err)
		}
		testBody(t, resp.Body, []byte(petition))
	}()
	is.Wait()
	want := fmt.Sprintf(golden(t.Name()), uri, is.req.RemoteAddr, ts.Listener.Addr())
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

func TestIncomingMultipartForm(t *testing.T) {
	t.Parallel()
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
	is := inspect(logger.Middleware(multipartHandler{t}), 1)

	ts := httptest.NewServer(is)
	defer ts.Close()
	uri := fmt.Sprintf("%s/multipart-upload", ts.URL)
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	multipartTestdata(writer, body)
	go func() {
		client := newServerClient()
		req, err := http.NewRequest(http.MethodPost, uri, body)
		if err != nil {
			t.Errorf("cannot create request: %v", err)
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())
		if _, err = client.Do(req); err != nil {
			t.Errorf("cannot connect to the server: %v", err)
		}
	}()
	is.Wait()
	want := fmt.Sprintf(golden(t.Name()), uri, is.req.RemoteAddr, ts.Listener.Addr(), writer.FormDataContentType())
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

func TestIncomingTLS(t *testing.T) {
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
	is := inspect(logger.Middleware(helloHandler{}), 1)

	ts := httptest.NewTLSServer(is)
	defer ts.Close()
	go func() {
		client := ts.Client()
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
		testBody(t, resp.Body, []byte("Hello, world!"))
	}()
	is.Wait()
	want := fmt.Sprintf(golden(t.Name()), is.req.RemoteAddr)
	if got := buf.String(); !regexp.MustCompile(want).MatchString(got) {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

func TestIncomingMutualTLS(t *testing.T) {
	t.Parallel()
	caCert, err := os.ReadFile("testdata/cert.pem")
	if err != nil {
		panic(err)
	}
	clientCert, err := os.ReadFile("testdata/cert-client.pem")
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
	logger := &Logger{
		TLS:            true,
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	is := inspect(logger.Middleware(helloHandler{}), 1)

	// NOTE(henvic): Using httptest directly turned out complicated.
	// See https://venilnoronha.io/a-step-by-step-guide-to-mtls-in-go
	server := &http.Server{
		TLSConfig: tlsConfig,
		Handler:   is,
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
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		panic(err)
	}

	host := fmt.Sprintf("https://localhost:%s/mutual-tls-test", port)
	go func() {
		transport := newTransport()
		transport.TLSClientConfig = clientTLSConfig
		client := &http.Client{
			Transport: transport,
		}
		resp, err := client.Get(host)
		if err != nil {
			t.Errorf("cannot create request: %v", err)
		}
		testBody(t, resp.Body, []byte("Hello, world!"))
	}()
	is.Wait()
	want := fmt.Sprintf(golden(t.Name()), host, is.req.RemoteAddr, port)
	if got := buf.String(); !regexp.MustCompile(want).MatchString(got) {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

func TestIncomingMutualTLSNoSafetyLogging(t *testing.T) {
	t.Parallel()
	caCert, err := os.ReadFile("testdata/cert.pem")
	if err != nil {
		panic(err)
	}
	clientCert, err := os.ReadFile("testdata/cert-client.pem")
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
	logger := &Logger{
		// TLS must be false
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	}
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	is := inspect(logger.Middleware(helloHandler{}), 1)

	// NOTE(henvic): Using httptest directly turned out complicated.
	// See https://venilnoronha.io/a-step-by-step-guide-to-mtls-in-go
	server := &http.Server{
		TLSConfig: tlsConfig,
		Handler:   is,
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
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		panic(err)
	}
	host := fmt.Sprintf("https://localhost:%s/mutual-tls-test", port)
	go func() {
		transport := newTransport()
		transport.TLSClientConfig = clientTLSConfig
		client := &http.Client{
			Transport: transport,
		}
		resp, err := client.Get(host)
		if err != nil {
			t.Errorf("cannot create request: %v", err)
		}
		testBody(t, resp.Body, []byte("Hello, world!"))
	}()
	is.Wait()
	want := fmt.Sprintf(golden(t.Name()), host, is.req.RemoteAddr, port)
	if got := buf.String(); got != want {
		t.Errorf("logged HTTP request %s; want %s", got, want)
	}
}

func newServerClient() *http.Client {
	return &http.Client{
		Transport: newTransport(),
	}
}
