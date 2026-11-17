package cip

import "testing"

func TestNormalizePage(t *testing.T) {
	cases := []struct{ inIndex, inSize, wantIndex, wantSize int }{
		{0, 0, 1, 20},
		{-3, 0, 1, 20},
		{3, 10, 3, 10},
		{1, 201, 1, 200},
	}
	for _, tc := range cases {
		index, size := normalizePage(tc.inIndex, tc.inSize)
		if index != tc.wantIndex || size != tc.wantSize {
			t.Fatalf("normalizePage(%d,%d) = %d,%d want %d,%d",
				tc.inIndex, tc.inSize, index, size, tc.wantIndex, tc.wantSize)
		}
	}
}
