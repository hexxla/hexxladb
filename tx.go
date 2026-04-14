package hexxladb

// Tx is a database transaction or snapshot view. Obtain it only from [DB.View], [DB.Update], or [DB.Batch].
// A Tx is valid only for the duration of the callback; do not store it past the callback return.
type Tx struct {
	db       *DB
	writable bool
}

// View runs fn inside a read-only transaction. Many concurrent View calls are allowed;
// they exclude only an active [Update] or [Batch]. See docs/hexxladb/TX.md.
func (db *DB) View(fn func(*Tx) error) error {
	if fn == nil {
		return ErrNilCallback
	}
	if db == nil {
		return ErrDatabaseClosed
	}
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.activeEng() == nil {
		return ErrDatabaseClosed
	}
	tx := &Tx{db: db, writable: false}
	return fn(tx)
}

// Update runs fn inside a read-write transaction. Exclusive: no concurrent View, Update, or Batch.
func (db *DB) Update(fn func(*Tx) error) error {
	if fn == nil {
		return ErrNilCallback
	}
	if db == nil {
		return ErrDatabaseClosed
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.activeEng() == nil {
		return ErrDatabaseClosed
	}
	tx := &Tx{db: db, writable: true}
	return fn(tx)
}

// Batch runs fn inside a read-write transaction. It is equivalent to [DB.Update]; the name
// matches spec and Bolt-style expectations for a batched write entrypoint. Semantics and
// locking are identical to Update (exclusive writer).
func (db *DB) Batch(fn func(*Tx) error) error {
	return db.Update(fn)
}

// Get returns the value for key in the ordered store, or (nil, false, nil) if missing.
func (tx *Tx) Get(key []byte) (val []byte, ok bool, err error) {
	if tx == nil || tx.db == nil {
		return nil, false, ErrClosed
	}
	e := tx.db.activeEng()
	if e == nil {
		return nil, false, ErrDatabaseClosed
	}
	return tx.db.btree.Get(key)
}

// Put inserts or replaces a key/value pair. Only allowed inside [DB.Update].
func (tx *Tx) Put(key, val []byte) error {
	if tx == nil || tx.db == nil {
		return ErrClosed
	}
	if !tx.writable {
		return ErrTxReadOnly
	}
	e := tx.db.activeEng()
	if e == nil {
		return ErrDatabaseClosed
	}
	return tx.db.btree.Put(key, val)
}

// AscendRange calls fn for keys in [from, to] inclusive (byte order). If from is nil, starts at the smallest key.
func (tx *Tx) AscendRange(from, to []byte, fn func(k, v []byte) bool) error {
	if tx == nil || tx.db == nil {
		return ErrClosed
	}
	e := tx.db.activeEng()
	if e == nil {
		return ErrDatabaseClosed
	}
	return tx.db.btree.AscendRange(from, to, fn)
}

// Writable reports whether this transaction was started with [DB.Update].
func (tx *Tx) Writable() bool {
	return tx != nil && tx.writable
}
