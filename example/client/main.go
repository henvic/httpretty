package main

import (
	"fmt"
	"net/http"
	"os"

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

	http.DefaultClient.Transport = logger.RoundTripper(http.DefaultTransport)

	if _, err := http.Get("https://www.google.com/"); err != nil {
		fmt.Fprintf(os.Stderr, "%+v\n", err)
		os.Exit(1)
	}
}
