package hexxladb

// PutRawFixture seeds physical rows for tests that exercise corrupt or legacy data.
func PutRawFixture(tx *Tx, key, value []byte) error {
	return tx.putDirect(key, value)
}
