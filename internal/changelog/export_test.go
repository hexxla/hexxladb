package changelog

// AppendTestEntries uses production intent preparation and projection for fixtures.
func AppendTestEntries(log *Log, wallUnixNs int64, entries []Entry) error {
	intents := make([]Intent, 0, len(entries))
	for _, entry := range entries {
		intent, err := PrepareIntent(wallUnixNs, entry, MaxInlinePayload+intentFixedSize)
		if err != nil {
			return err
		}
		intents = append(intents, intent)
	}
	return log.AppendIntents(intents)
}

func AppendTestEntry(log *Log, wallUnixNs int64, op byte, key, encoded []byte) error {
	return AppendTestEntries(log, wallUnixNs, []Entry{{Op: op, Key: key, Encoded: encoded}})
}

func encodeInner(seq uint64, wallUnixNs int64, op byte, key, encoded []byte) ([]byte, error) {
	intent, err := PrepareIntent(wallUnixNs, Entry{Op: op, Key: key, Encoded: encoded}, MaxInlinePayload+intentFixedSize)
	if err != nil {
		return nil, err
	}
	return encodeRecordInner(intentRecord(intent, seq))
}
