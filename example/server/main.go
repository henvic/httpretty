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
	}

	addr := ":8090"
	fmt.Printf("Open http://localhost%s in the browser.\n", addr)
	if err := http.ListenAndServe(addr, logger.Middleware(helloHandler{})); err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, err)
	}
}

type helloHandler struct{}

func (h helloHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header()["Date"] = nil
	fmt.Fprintf(w, "Hello, world!")
}
