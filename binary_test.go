package httpretty

import (
	"bytes"
	"testing"
)

func TestIsBinary(t *testing.T) {
	testCases := []struct {
		desc   string
		data   []byte
		binary bool
	}{
		{
			desc:   "Empty",
			binary: false,
		},
		{
			desc:   "Text",
			data:   []byte("plain text"),
			binary: false,
		},
		{
			desc:   "More text",
			data:   []byte("plain text\n"),
			binary: false,
		},
		{
			desc:   "Text with UTF16 Big Endian BOM",
			data:   []byte("\xFE\xFFevil plain text"),
			binary: false,
		},
		{
			desc:   "Text with UTF16 Little Endian BOM",
			data:   []byte("\xFF\xFEevil plain text"),
			binary: false,
		},
		{
			desc:   "Text with UTF8 BOM",
			data:   []byte("\xEF\xBB\xBFevil plain text"),
			binary: false,
		},
		{
			desc:   "Binary",
			data:   []byte{1, 2, 3},
			binary: true,
		},
		{
			desc:   "Binary over 512bytes",
			data:   bytes.Repeat([]byte{1, 2, 3, 4, 5, 6, 7, 8}, 65),
			binary: true,
		},
		{
			desc:   "JPEG image",
			data:   []byte("\xFF\xD8\xFF"),
			binary: true,
		},
		{
			desc:   "AVI video",
			data:   []byte("RIFF,O\n\x00AVI LISTÀ"),
			binary: true,
		},
		{
			desc:   "RAR",
			data:   []byte("Rar!\x1A\x07\x00"),
			binary: true,
		},
		{
			desc:   "PDF",
			data:   []byte("\x25\x50\x44\x46\x2d\x31\x2e\x33\x0a\x25\xc4\xe5\xf2\xe5\xeb\xa7"),
			binary: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			if got := isBinary(tc.data); got != tc.binary {
				t.Errorf("wanted isBinary(%v) = %v, got %v instead", tc.data, tc.binary, got)
			}
		})
	}
}
