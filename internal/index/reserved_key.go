package index

import "bytes"

// IsReservedKey reports whether a key belongs to a database-owned family.
func IsReservedKey(key []byte) bool {
	end := bytes.IndexByte(key, '/')
	if end < 0 {
		return false
	}
	switch string(key[:end+1]) {
	case CellPrefix, FacetPrefix, EdgePrefix, SeamPrefix, SeamByCellsPrefix,
		SeamSourcePrefix, SeamTimePrefix, SourcePrefix, TimePrefix, TagPrefix,
		EmbedPrefix, "hnsw/", "__meta/":
		return true
	default:
		return false
	}
}
