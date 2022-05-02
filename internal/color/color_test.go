package color

import (
	"reflect"
	"testing"
)

func TestFormat(t *testing.T) {
	want := "\x1b[102;95mHello World\x1b[0m"
	got := Format(BgHiGreen, FgHiMagenta, "Hello World")
	if got != want {
		t.Errorf("Expecting %s, got '%s'\n", want, got)
	}
}

func TestMalformedFormat(t *testing.T) {
	want := "\x1b[102mHello World(EXTRA color.Attribute=95)\x1b[0m"
	got := Format(BgHiGreen, "Hello World", FgHiMagenta)
	if got != want {
		t.Errorf("Expecting %s, got '%s'\n", want, got)
	}
}

func TestMalformedSliceFormat(t *testing.T) {
	want := "\x1b[102mHello World(EXTRA color.Attribute=[95 41])\x1b[0m"
	got := Format(BgHiGreen, "Hello World", []Attribute{FgHiMagenta, BgRed})
	if got != want {
		t.Errorf("Expecting %s, got '%s'\n", want, got)
	}
}

func TestFormatSlice(t *testing.T) {
	format := []Attribute{BgHiGreen, FgHiMagenta}
	want := "\x1b[102;95mHello World\x1b[0m"
	got := Format(format, "Hello World")
	if got != want {
		t.Errorf("Expecting %s, got '%s'\n", want, got)
	}
}

func TestEmpty(t *testing.T) {
	if want, got := "", Format(); got != want {
		t.Errorf("Expecting %s, got '%s'\n", want, got)
	}
}

func TestEmptyColorString(t *testing.T) {
	if want, got := "", Format(BgBlack); got != want {
		t.Errorf("Expecting %s, got '%s'\n", want, got)
	}
}

func TestNoFormat(t *testing.T) {
	want := "\x1b[mHello World\x1b[0m"
	got := Format("Hello World")
	if got != want {
		t.Errorf("Expecting %s, got '%s'\n", want, got)
	}
}

func TestFormatStartingWithNumber(t *testing.T) {
	want := "\x1b[102;95m100 forks\x1b[0m"
	number := 100
	if reflect.TypeOf(number).String() != "int" {
		t.Errorf("Must be integer; not a similar like Attribute")
	}
	if got := Format(BgHiGreen, FgHiMagenta, number, " forks"); got != want {
		t.Errorf("Expecting %s, got '%s'\n", want, got)
	}
}

func TestFormatCtrlChar(t *testing.T) {
	if want, got := "\x1b[ma%b\x1b[0m", Format("a%b"); got != want {
		t.Errorf(`expected Format(a%%b) to be %q, got %q instead`, want, got)
	}
	if want, got := "\\x1b[34;46ma%b\\x1b[0m", Escape(Format(FgBlue, BgCyan, "a%b")); got != want {
		t.Errorf(`expected escaped formatted a%%b to be %q, got %q instead`, want, got)
	}
}

func TestEscape(t *testing.T) {
	unescaped := "\x1b[32mGreen"
	escaped := "\\x1b[32mGreen"
	if got := Escape(unescaped); got != escaped {
		t.Errorf("Expecting %s, got '%s'\n", escaped, got)
	}
}

func TestStripAttributes(t *testing.T) {
	want := "this is a regular string"
	got := StripAttributes(FgCyan, []Attribute{FgBlack}, "this is a regular string")
	if got != want {
		t.Errorf("StripAttributes(input) = %s, wanted %s", got, want)
	}
}

func TestStripAttributesEmpty(t *testing.T) {
	if got := StripAttributes(); got != "" {
		t.Errorf("StripAttributes() should work")
	}
}

func TestStripAttributesFirstParam(t *testing.T) {
	want := "foo (EXTRA color.Attribute=32)"
	got := StripAttributes("foo ", FgGreen)
	if got != want {
		t.Errorf(`expected StripAttributes = %v, got %v instead`, want, got)
	}
}

func TestStripAttributesSame(t *testing.T) {
	want := "this is a regular string"
	got := StripAttributes(want)
	if got != want {
		t.Errorf("StripAttributes(%s) = %s, wanted %s", want, got, want)
	}
}

func TestStripAttributesWithExtraColorAttribute(t *testing.T) {
	want := "this is a regular string (EXTRA color.Attribute=91) with an invalid color Attribute field"
	got := StripAttributes(BgCyan, []Attribute{FgBlack}, "this is a regular string ", FgHiRed, " with an invalid color Attribute field")
	if got != want {
		t.Errorf("StripAttributes(input) = %s, wanted %s", got, want)
	}
}
