package main

import (
	"bufio"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"

	"github.com/henvic/httpretty"
)

func main() {
	logger := &httpretty.Logger{
		Time:           true,
		TLS:            true,
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
		Colors:         true, // erase line if you don't like colors
		Formatters:     []httpretty.Formatter{&httpretty.JSONFormatter{}},
	}

	// Using a custom HTTP client using the logger RoundTripper, rather than http.DefaultClient.
	client := &http.Client{
		Transport: logger.RoundTripper(http.DefaultTransport),
	}

	fmt.Print("httprepl is a small HTTP client REPL (read-eval-print-loop) program example\n\n")
	help()
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("$ ")
		readEvalPrint(reader, client)
	}
}

func readEvalPrint(reader *bufio.Reader, client *http.Client) {
	s, err := reader.ReadString('\n')
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot read stdin: %v\n", err)
		os.Exit(1)
	}

	if runtime.GOOS == "windows" {
		s = strings.TrimRight(s, "\r\n")
	} else {
		s = strings.TrimRight(s, "\n")
	}
	s = strings.TrimSpace(s)

	switch {
	case s == "exit":
		os.Exit(0)
	case s == "help":
		help()
		return
	case s == "":
		return
	case s == "get":
		fmt.Fprintln(os.Stderr, "missing address")
	case !strings.HasPrefix(s, "get "):
		fmt.Fprint(os.Stderr, "invalid command\n\n")
		return
	}

	s = strings.TrimPrefix(s, "get ")
	uri, err := url.Parse(s)
	if err == nil && uri.Scheme == "" {
		uri.Scheme = "http"
		s = uri.String()
	}

	// we just ignore the request contents but you can see it printed thanks to the logger.
	if _, err := client.Get(s); err != nil {
		fmt.Fprintf(os.Stderr, "%+v\n\n", err)
	}
	fmt.Println()
}

func help() {
	fmt.Print(`Commands available:
get <address>	URL to get. Example: "get www.google.com"
help		This command list
exit		Quit the application

`)
}
