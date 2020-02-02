package body

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"

	"github.com/henvic/httpretty/internal/color"
)

// Print body.
func Print(w io.Writer, body io.Reader) {
	var err error

	switch body.(type) {
	case nil:
	case *os.File:
		err = printFilename(w, body)
	case *bytes.Buffer:
		err = printBuffer(w, body)
	case *bytes.Reader:
		err = printBytes(w, body)
	case *strings.Reader:
		err = printStrings(w, body)
	default:
		err = printUnknown(w, body)
	}

	if err != nil {
		fmt.Fprintf(w, "error printing body: %+v\n", err)
	}
}

func printFilename(w io.Writer, body io.Reader) (err error) {
	f := body.(*os.File)
	_, err = fmt.Fprintln(w, color.Format(color.FgMagenta, "request body: sending file %v", f.Name()))
	return err
}

func printBuffer(w io.Writer, body io.Reader) (err error) {
	_, err = fmt.Fprintf(w, "\n%s\n", body.(*bytes.Buffer))
	return err
}

func printBytes(w io.Writer, body io.Reader) (err error) {
	var reader = body.(*bytes.Reader)
	var b bytes.Buffer

	if _, err = reader.Seek(0, 0); err != nil {
		return err
	}

	if _, err = reader.WriteTo(&b); err != nil {
		return err
	}

	_, err = fmt.Fprintf(w, "\n%s\n", b.String())
	return err
}

func printStrings(w io.Writer, body io.Reader) (err error) {
	var reader = body.(*strings.Reader)
	var b bytes.Buffer

	if _, err = reader.Seek(0, 0); err != nil {
		return err
	}

	if _, err = reader.WriteTo(&b); err != nil {
		return err
	}

	_, err = fmt.Fprintf(w, "\n%s\n", b.String())
	return err
}

func printUnknown(w io.Writer, body io.Reader) (err error) {
	_, err = fmt.Fprintf(w, "\n%s\n", color.Format(color.FgRed,
		"(request body: "+reflect.TypeOf(body).String()+")"))
	return err
}
