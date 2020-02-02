// Package color can be used to add color to your terminal using ANSI escape code (or sequences).
//
// See https://en.wikipedia.org/wiki/ANSI_escape_code
// Copy modified from https://github.com/fatih/color
// Copyright 2013 Fatih Arslan
package color

import (
	"fmt"
	"strconv"
	"strings"
)

// Attribute defines a single SGR (Select Graphic Rendition) code.
type Attribute int

// Base attributes
const (
	Reset Attribute = iota
	Bold
	Faint
	Italic
	Underline
	BlinkSlow
	BlinkRapid
	ReverseVideo
	Concealed
	CrossedOut
)

// Foreground text colors
const (
	FgBlack Attribute = iota + 30
	FgRed
	FgGreen
	FgYellow
	FgBlue
	FgMagenta
	FgCyan
	FgWhite
)

// Foreground Hi-Intensity text colors
const (
	FgHiBlack Attribute = iota + 90
	FgHiRed
	FgHiGreen
	FgHiYellow
	FgHiBlue
	FgHiMagenta
	FgHiCyan
	FgHiWhite
)

// Background text colors
const (
	BgBlack Attribute = iota + 40
	BgRed
	BgGreen
	BgYellow
	BgBlue
	BgMagenta
	BgCyan
	BgWhite
)

// Background Hi-Intensity text colors
const (
	BgHiBlack Attribute = iota + 100
	BgHiRed
	BgHiGreen
	BgHiYellow
	BgHiBlue
	BgHiMagenta
	BgHiCyan
	BgHiWhite
)

const (
	escape   = "\x1b"
	unescape = "\\x1b"
)

// Format text for terminal.
func Format(s ...interface{}) string {
	if len(s) == 0 {
		return ""
	}

	out := make([]interface{}, 0)
	params := []Attribute{}
	in := -1

	for i, v := range s {
		switch vt := v.(type) {
		case []Attribute:
			params = append(params, vt...)
		case Attribute:
			params = append(params, vt)
		default:
			in = i
			goto over
		}
	}

over:
	if in != -1 {
		out = s[in:]
	}

	if len(out) == 0 {
		return ""
	}

	return wrap(params, sprintf(out...))
}

// StripAttributes from input arguments and return unformatted text.
func StripAttributes(s ...interface{}) (raw string) {
	for k, v := range s {
		switch v.(type) {
		case []Attribute, Attribute:
		default:
			return sprintf(s[k:]...)
		}
	}

	return
}

// Escape text for terminal.
func Escape(s string) string {
	return strings.Replace(s, escape, unescape, -1)
}

func sprintf(s ...interface{}) string {
	format := s[0]
	return fmt.Sprintf(fmt.Sprintf("%v", format), s[1:]...)
}

// sequence returns a formated SGR sequence to be plugged into a "\x1b[...m"
// an example output might be: "1;36" -> bold cyan.
func sequence(params []Attribute) string {
	format := make([]string, len(params))
	for i, v := range params {
		format[i] = strconv.Itoa(int(v))
	}

	return strings.Join(format, ";")
}

// wrap the s string with the colors attributes.
func wrap(params []Attribute, s string) string {
	return fmt.Sprintf("%s[%sm%s%s[%dm", escape, sequence(params), s, escape, Reset)
}
