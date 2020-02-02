// +build race

package httpretty

// Race is a flag that can be usde to detect whether the race detector is on.
// It was added because as of Go 1.13.7 the TestOutgoingConcurrency test is failing because of a bug on the
// net/http standard library package.
// See https://golang.org/issue/30597
var Race = true
