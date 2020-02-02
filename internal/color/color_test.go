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

func TestFormatLong(t *testing.T) {
	want := "\x1b[102;95mHello World, my little 92%!(EXTRA string=robot)\x1b[0m"
	got := Format(BgHiGreen, FgHiMagenta, "Hello World, %s %s %v", "my", "little", FgHiGreen, "robot")

	if got != want {
		t.Errorf("Expecting %s, got '%s'\n", want, got)
	}
}

func TestMalformedFormat(t *testing.T) {
	want := "\x1b[102mHello World%!(EXTRA color.Attribute=95)\x1b[0m"
	got := Format(BgHiGreen, "Hello World", FgHiMagenta)

	if got != want {
		t.Errorf("Expecting %s, got '%s'\n", want, got)
	}
}

func TestFormatArray(t *testing.T) {
	format := []Attribute{BgHiGreen, FgHiMagenta}

	want := "\x1b[102;95mHello World\x1b[0m"
	got := Format(format, "Hello World")

	if got != want {
		t.Errorf("Expecting %s, got '%s'\n", want, got)
	}
}

func TestEmpty(t *testing.T) {
	want := ""
	got := Format()

	if got != want {
		t.Errorf("Expecting %s, got '%s'\n", want, got)
	}
}

func TestEmptyColorString(t *testing.T) {
	want := ""
	got := Format(BgBlack)

	if got != want {
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

	got := Format(BgHiGreen, FgHiMagenta, "%v forks", number)

	if got != want {
		t.Errorf("Expecting %s, got '%s'\n", want, got)
	}
}

func TestFormatAsSprintf(t *testing.T) {
	want := "\x1b[102;95mHello World\x1b[0m"
	got := Format(BgHiGreen, FgHiMagenta, "%v", "Hello World")

	if got != want {
		t.Errorf("Expecting %s, got '%s'\n", want, got)
	}
}

func TestEscape(t *testing.T) {
	unescaped := "\x1b[32mGreen"
	escaped := "\\x1b[32mGreen"
	got := Escape(unescaped)

	if got != escaped {
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

func TestStripAttributesSame(t *testing.T) {
	want := "this is a regular string"
	got := StripAttributes(want)

	if got != want {
		t.Errorf("StripAttributes(%s) = %s, wanted %s", want, got, want)
	}
}
