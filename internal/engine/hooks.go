package engine

// PageHooks optional transforms at the page boundary (plaintext default).
// When AES-XTS encryption is enabled, pageID is used as the sector tweak; page 0 is never transformed.
type PageHooks struct {
	// PhysicalPageOverhead is added to the logical page size for transformed
	// data pages. It is zero for plaintext, custom, and legacy XTS transforms.
	PhysicalPageOverhead int
	// BeforeWrite runs before a page is logged to the WAL and written to the primary file.
	BeforeWrite func(pageID uint64, plain []byte) (out []byte, err error)
	// BeforeWriteVersioned receives the durable WAL sequence used as the page
	// rewrite generation. When set it takes precedence over BeforeWrite.
	BeforeWriteVersioned func(pageID, generation uint64, plain []byte) (out []byte, err error)
	// AfterRead runs after a page is read from the primary file.
	AfterRead func(pageID uint64, data []byte) (out []byte, err error)
}

func (h PageHooks) transformWrite(pageID, generation uint64, b []byte) ([]byte, error) {
	if h.BeforeWriteVersioned != nil {
		return h.BeforeWriteVersioned(pageID, generation, b)
	}
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
