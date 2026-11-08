package lattice_test

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/hexxla/hexxladb/internal/lattice"
)

func TestFieldOfViewReciprocityOracle(t *testing.T) {
	t.Parallel()
	points := lattice.WalkRings(nil, lattice.Coord{}, 4)
	rng := rand.New(rand.NewPCG(31, 101))
	for trial := range 80 {
		blockers := map[lattice.Coord]bool{}
		for _, c := range points {
			if rng.IntN(5) == 0 {
				blockers[c] = true
			}
		}
		if trial == 0 {
			blockers = map[lattice.Coord]bool{{Q: 1}: true}
		}
		sight := map[lattice.Coord]map[lattice.Coord]bool{}
		for _, a := range points {
			if blockers[a] {
				continue
			}
			sight[a] = map[lattice.Coord]bool{}
			for _, b := range lattice.FieldOfView(a, 5, func(c lattice.Coord) bool { return blockers[c] }) {
				sight[a][b] = true
			}
		}
		for a, seen := range sight {
			for b, other := range sight {
				if seen[b] != other[a] {
					t.Fatalf("trial=%d: %v to %v=%t, reverse=%t, blockers=%v", trial, a, b, seen[b], other[a], blockers)
				}
			}
		}
	}
}

// clipHexSegment independently intersects the closed centre-to-centre segment
// with the six half-planes of an opaque hex. Fractions keep boundary cases exact.
func clipHexSegment(origin, target, blocker lattice.Coord) bool {
	direction := lattice.Coord{Q: target.Q - origin.Q, R: target.R - origin.R}
	centre := lattice.Coord{Q: blocker.Q - origin.Q, R: blocker.R - origin.R}
	lower, upper := [2]int{0, 1}, [2]int{1, 1}
	for _, normal := range (lattice.Coord{}).Neighbors() {
		a := (2*direction.Q+direction.R)*normal.Q + (direction.Q+2*direction.R)*normal.R
		bound := 1 + (2*centre.Q+centre.R)*normal.Q + (centre.Q+2*centre.R)*normal.R
		switch {
		case a == 0:
			if bound < 0 {
				return false
			}
		case a > 0:
			if bound*upper[1] < upper[0]*a {
				upper = [2]int{bound, a}
			}
		default:
			if -bound*lower[1] > lower[0]*-a {
				lower = [2]int{-bound, -a}
			}
		}
	}
	return lower[0]*upper[1] <= upper[0]*lower[1]
}

func TestFieldOfViewMatchesHexPolygonOracle(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewPCG(61, 2026))
	for trial := range 40 {
		origin := lattice.Coord{Q: 1_000_000_000 + trial, R: -1_000_000_000}
		points := lattice.WalkRings(nil, origin, 5)
		blockers := map[lattice.Coord]bool{}
		for _, point := range points[1:] {
			if rng.IntN(6) == 0 {
				blockers[point] = true
			}
		}
		seen := map[lattice.Coord]bool{}
		for _, point := range lattice.FieldOfView(origin, 5, func(c lattice.Coord) bool { return blockers[c] }) {
			seen[point] = true
		}
		for _, target := range points {
			want := true
			for blocker := range blockers {
				if blocker != target && clipHexSegment(origin, target, blocker) {
					want = false
					break
				}
			}
			if seen[target] != want {
				t.Fatalf("trial %d target %v visible=%t, polygon oracle=%t", trial, target, seen[target], want)
			}
		}
	}
}

func TestFieldOfViewHexSymmetries(t *testing.T) {
	t.Parallel()
	blockers := map[lattice.Coord]bool{{Q: 1}: true, {Q: -2, R: 1}: true, {Q: 2, R: -3}: true}
	original := lattice.FieldOfView(lattice.Coord{}, 5, func(c lattice.Coord) bool { return blockers[c] })
	for transform := range 12 {
		convert := func(c lattice.Coord) lattice.Coord {
			if transform >= 6 {
				c.Q, c.R = c.R, c.Q
			}
			for range transform % 6 {
				c = lattice.Coord{Q: -c.R, R: c.Q + c.R}
			}
			return c
		}
		transformed := map[lattice.Coord]bool{}
		for c := range blockers {
			transformed[convert(c)] = true
		}
		got := map[lattice.Coord]bool{}
		for _, c := range lattice.FieldOfView(lattice.Coord{}, 5, func(c lattice.Coord) bool { return transformed[c] }) {
			got[c] = true
		}
		if len(got) != len(original) {
			t.Fatalf("transform %d changes visible count from %d to %d", transform, len(original), len(got))
		}
		for _, c := range original {
			if !got[convert(c)] {
				t.Fatalf("transform %d loses %v", transform, c)
			}
		}
	}
}

func BenchmarkFieldOfViewBlockers(b *testing.B) {
	for _, radius := range []int{20, 50, 256} {
		b.Run(fmt.Sprint(radius), func(b *testing.B) {
			opaque := func(c lattice.Coord) bool { return c == (lattice.Coord{Q: 2}) }
			for b.Loop() {
				lattice.FieldOfView(lattice.Coord{}, radius, opaque)
			}
		})
	}
}
