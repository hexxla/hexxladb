package engine

// PageSize is the fixed on-disk page size for v1 (64 KiB).
const PageSize = 64 << 10

const (
	headerMagic      = "HEXXLADB"
	headerPrefixSize = 512
	formatVersionV1  = uint32(1)

	btreeNodeMagic = "HXBT"
)
