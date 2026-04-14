package lattice

import "testing"

func BenchmarkPack(b *testing.B) {
	c := Coord{Q: 12, R: -5}
	b.ResetTimer()
	for b.Loop() {
		_, _ = Pack(c)
	}
}

func BenchmarkUnpack(b *testing.B) {
	c := Coord{Q: 12, R: -5}
	p, err := Pack(c)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for b.Loop() {
		_, _ = Unpack(p)
	}
}

func BenchmarkPackedCompare(b *testing.B) {
	a, err := Pack(Coord{Q: 1, R: 0})
	if err != nil {
		b.Fatal(err)
	}
	z, err := Pack(Coord{Q: 2, R: -1})
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for b.Loop() {
		_ = a.Compare(z)
	}
}

func BenchmarkDistance(b *testing.B) {
	a := Coord{Q: 0, R: 0}
	o := Coord{Q: 100, R: -50}
	b.ResetTimer()
	for b.Loop() {
		_ = a.Distance(o)
	}
}
