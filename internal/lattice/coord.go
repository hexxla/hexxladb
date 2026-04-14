package lattice

// Coord is an axial coordinate (q, r). Cube coordinate s is -q - r.
type Coord struct {
	Q int
	R int
}

// Cube holds cube coordinates with constraint q + r + s == 0.
type Cube struct {
	Q int
	R int
	S int
}

// Cube converts axial coordinates to cube coordinates per HEXXLA.md (Geometric Model).
func (c Coord) Cube() Cube {
	return Cube{Q: c.Q, R: c.R, S: -c.Q - c.R}
}

// Distance returns the hex grid distance between two axial coordinates (exact step count).
// Formula matches docs/hexxladb/HEXXLA.md (axial / cube Manhattan halved).
func (c Coord) Distance(other Coord) int {
	dq := c.Q - other.Q
	dr := c.R - other.R
	ds := (c.Q + c.R) - (other.Q + other.R)
	return (abs(dq) + abs(dr) + abs(ds)) / 2
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
