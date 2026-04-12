package hash_test

import (
	"errors"
	"testing"

	"github.com/sploitzberg/go-hexagonal-architecture-template/internal/domain"
	"github.com/sploitzberg/go-hexagonal-architecture-template/internal/domain/hash"
)

func TestSHA256Hex(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "hello",
			in:   "hello",
			want: "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
		},
		{
			name: "empty",
			in:   "",
			want: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			name: "abc",
			in:   "abc",
			want: "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := hash.SHA256Hex(tc.in)
			if err != nil {
				t.Fatalf("SHA256Hex: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSHA256Hex_tooLarge(t *testing.T) {
	t.Parallel()
	msg := string(make([]byte, domain.MaxContentLen+1))
	_, err := hash.SHA256Hex(msg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, domain.ErrContentTooLarge) {
		t.Fatalf("expected ErrContentTooLarge, got %v", err)
	}
}
