package index

import (
	"bytes"
	"errors"
	"testing"

	"github.com/hexxla/hexxladb/internal/lattice"
)

func TestEdgeKey_roundTrip(t *testing.T) {
	t.Parallel()
	from, err := lattice.Pack(lattice.Coord{Q: 0, R: 0})
	if err != nil {
		t.Fatal(err)
	}
	to, err := lattice.Pack(lattice.Coord{Q: 1, R: -1})
	if err != nil {
		t.Fatal(err)
	}
	k, err := EdgeKey(from, to, "adjacent")
	if err != nil {
		t.Fatal(err)
	}
	gFrom, gTo, rt, err := ParseEdgeKey(k)
	if err != nil {
		t.Fatal(err)
	}
	if gFrom != from || gTo != to || rt != "adjacent" {
		t.Fatalf("got %+v %+v %q", gFrom, gTo, rt)
	}
}

func TestEdgeKey_emptyRelationType(t *testing.T) {
	t.Parallel()
	a, _ := lattice.Pack(lattice.Coord{Q: 0, R: 0})
	b, _ := lattice.Pack(lattice.Coord{Q: 1, R: 0})
	_, err := EdgeKey(a, b, "")
	if !errors.Is(err, ErrEmptyRelationType) {
		t.Fatalf("got %v", err)
	}
}

func TestEdgeKey_ordering(t *testing.T) {
	t.Parallel()
	c0, _ := lattice.Pack(lattice.Coord{Q: 0, R: 0})
	c1, _ := lattice.Pack(lattice.Coord{Q: 1, R: 0})
	c2, _ := lattice.Pack(lattice.Coord{Q: 2, R: 0})
	k01a, _ := EdgeKey(c0, c1, "a")
	k01b, _ := EdgeKey(c0, c1, "b")
	if bytes.Compare(k01a, k01b) >= 0 {
		t.Fatal("same from/to: relation type a should sort before b")
	}
	k02, _ := EdgeKey(c0, c2, "a")
	if bytes.Compare(k01a, k02) >= 0 {
		t.Fatal("smaller to-coord should sort first")
	}
}

func TestEdgeFromPrefix(t *testing.T) {
	t.Parallel()
	from, _ := lattice.Pack(lattice.Coord{Q: 3, R: -2})
	p := EdgeFromPrefix(from)
	if !bytes.HasPrefix(kMustEdge(t, from, from, "loop"), p) {
		t.Fatal("edge from self should have prefix")
	}
}

func kMustEdge(t *testing.T, from, to lattice.PackedCoord, rel string) []byte {
	t.Helper()
	k, err := EdgeKey(from, to, rel)
	if err != nil {
		t.Fatal(err)
	}
	return k
}
