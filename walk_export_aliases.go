package hexxladb

import "github.com/hexxla/hexxladb/internal/record"

// FacetWalkRecord is an alias of the facet wire record so embedding apps (e.g. MCP adapters) can write
// [Tx.AscendFacetsForCell] callbacks without importing internal packages.
type FacetWalkRecord = record.FacetRecord

// EdgeWalkRecord is an alias of the edge wire record for [Tx.AscendEdgesFrom] callbacks.
type EdgeWalkRecord = record.EdgeRecord
