package cert

import "testing"

func TestNormalizePage(t *testing.T) {
	cases := []struct{ inIndex, inSize, wantIndex, wantSize int }{
		{0, 0, 1, 20},
		{-1, -1, 1, 20},
		{2, 50, 2, 50},
		{1, 500, 1, 200},
	}
	for _, tc := range cases {
		index, size := normalizePage(tc.inIndex, tc.inSize)
		if index != tc.wantIndex || size != tc.wantSize {
			t.Fatalf("normalizePage(%d,%d) = %d,%d want %d,%d",
				tc.inIndex, tc.inSize, index, size, tc.wantIndex, tc.wantSize)
		}
	}
}
