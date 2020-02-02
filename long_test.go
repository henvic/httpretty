package httpretty

import (
	"io/ioutil"
)

// from http://bastiat.org/fr/petition.html
var petition = func() string {
	content, err := ioutil.ReadFile("testdata/petition.golden")
	if err != nil {
		panic(err)
	}

	return string(content)
}()
