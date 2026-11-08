# HexxlaDB contradiction adapter

[Storage](storage.go) translates [application observations](../../../domain/storage.go)
to cell records and a conflict seam in one HexxlaDB transaction. A separate
transaction records the resolution.
