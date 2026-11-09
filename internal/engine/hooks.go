package engine

// PageHooks transforms authenticated data pages. Plaintext uses the zero value.
type PageHooks struct {
	// PhysicalPageOverhead is added to the logical page size for transformed
	// data pages. It is zero for plaintext.
	PhysicalPageOverhead int
	// BeforeWriteVersioned receives the durable WAL sequence as the page generation.
	BeforeWriteVersioned func(pageID, generation uint64, plain []byte) (out []byte, err error)
	// AfterRead runs after a page is read from the primary file.
	AfterRead func(pageID uint64, data []byte) (out []byte, err error)
}

func (h PageHooks) transformWrite(pageID, generation uint64, b []byte) ([]byte, error) {
	if h.BeforeWriteVersioned != nil {
		return h.BeforeWriteVersioned(pageID, generation, b)
	}
	return b, nil
}

func (h PageHooks) transformRead(pageID uint64, b []byte) ([]byte, error) {
	if h.AfterRead == nil {
		return b, nil
	}
	return h.AfterRead(pageID, b)
}
