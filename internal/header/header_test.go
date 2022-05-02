package header

import (
	"net/http"
	"reflect"
	"testing"
)

func TestSanitize(t *testing.T) {
	// no need to test request and response headers sanitization separately
	var headers = http.Header{}
	headers.Set("Accept", "*/*")
	headers.Set("User-Agent", "curl/7.54.0")
	headers.Add("Cookie", "abcd=secret1")
	headers.Add("Cookie", "xyz=secret2")
	headers.Add("Set-Cookie", "session_id=secret3")
	headers.Add("Set-Cookie", "id=a3fWa; Expires=Wed, 21 Oct 2015 07:28:00 GMT; Secure; HttpOnly")
	headers.Add("Authorization", "Bearer foo")
	headers.Add("Proxy-Authorization", "Basic Zm9vQGV4YW1wbGUuY29tOmJhcg==")
	headers.Set("Content-Type", "application/x-www-form-urlencoded")
	headers.Set("Content-Length", "3")

	var got = Sanitize(DefaultSanitizers, headers)
	if len(headers) != len(got) {
		t.Errorf("Expected length of sanitized headers (%d) to be equal to length of original headers (%d)", len(got), len(headers))
	}
	want := http.Header{
		"Accept":              []string{"*/*"},
		"User-Agent":          []string{"curl/7.54.0"},
		"Cookie":              []string{"abcd=████████████████████", "xyz=████████████████████"},
		"Set-Cookie":          []string{"session_id=████████████████████", "id=████████████████████; Expires=Wed, 21 Oct 2015 07:28:00 GMT; Secure; HttpOnly"},
		"Authorization":       []string{"Bearer ████████████████████"},
		"Proxy-Authorization": []string{"Basic ████████████████████"},
		"Content-Type":        []string{"application/x-www-form-urlencoded"},
		"Content-Length":      []string{"3"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Sanitized headers doesn't match expected value: wanted %+v, got %+v instead", want, got)
	}
}
