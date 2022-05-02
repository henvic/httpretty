//go:build race
// +build race

package httpretty

func init() {
	race = true
}
