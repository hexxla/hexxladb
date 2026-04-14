package engine

// PageHooks optional transforms at the page boundary (plaintext default).
// M9: AES-XTS encryption uses pageID as the sector tweak; page 0 is never transformed.
type PageHooks struct {
	// BeforeWrite runs before a page is logged to the WAL and written to the primary file.
	BeforeWrite func(pageID uint64, plain []byte) (out []byte, err error)
	// AfterRead runs after a page is read from the primary file.
	AfterRead func(pageID uint64, data []byte) (out []byte, err error)
}

func (h PageHooks) transformWrite(pageID uint64, b []byte) ([]byte, error) {
	if h.BeforeWrite == nil {
		return b, nil
	}
	return h.BeforeWrite(pageID, b)
}

func (h PageHooks) transformRead(pageID uint64, b []byte) ([]byte, error) {
	if h.AfterRead == nil {
		return b, nil
	}
	return h.AfterRead(pageID, b)
}
