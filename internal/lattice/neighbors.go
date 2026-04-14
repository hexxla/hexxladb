package lattice

// Neighbors returns the six adjacent axial coordinates (hex distance 1), in order:
// (+q), (+q,-r neighbor), … matching axial directions used in tests and ring walks.
func (c Coord) Neighbors() [6]Coord {
	return [6]Coord{
		{c.Q + 1, c.R},
		{c.Q + 1, c.R - 1},
		{c.Q, c.R - 1},
		{c.Q - 1, c.R},
		{c.Q - 1, c.R + 1},
		{c.Q, c.R + 1},
	}
}
