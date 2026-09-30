package evm

import (
	"encoding/hex"
	"testing"

	"github.com/guoyyit/evm-runtime-go/common"
)

func TestKeccak256Vector(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470"},
		{"abc", "4e03657aea45a94fc7d47ba826c8d667c0d1e6e33a64a036ec44f58fa12d6c45"},
		{"testing", "5f16f4c7f149ac4f9510d9cf8cf384038ad348b3bcdc01915f95de12df9d1b02"},
	}
	for _, c := range cases {
		got := hex.EncodeToString(common.Keccak256([]byte(c.in)))
		if got != c.want {
			t.Fatalf("keccak256(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}
