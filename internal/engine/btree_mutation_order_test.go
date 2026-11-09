package engine

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"testing"
)

func TestDeleteUnlinksLeafBeforePageReuse(t *testing.T) {
	t.Parallel()
	eng := openTestDB(t, authenticatedRecoveryTestOptions())
	tree := OpenBTree(eng)
	value := incompressibleBytes(3000)
	commitTreeMutation(t, eng, func() error {
		for _, key := range []string{"a", "c", "e", "g"} {
			if err := tree.Put([]byte(key), value); err != nil {
				return err
			}
		}
		return nil
	})
	commitTreeMutation(t, eng, func() error { return tree.Delete([]byte("c")) })
	commitTreeMutation(t, eng, func() error { return tree.Put([]byte("z"), value) })
	verifyTreeModel(t, tree, map[string][]byte{"a": value, "e": value, "g": value, "z": value})
}

func TestBTreeMixedOrderInsertDeleteAndReopen(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"plaintext", "authenticated"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			opts := &Options{}
			if mode == "authenticated" {
				opts = authenticatedRecoveryTestOptions()
			}
			path := filepath.Join(t.TempDir(), "mixed.db")
			eng, err := Open(path, opts)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = eng.Close() })
			tree := OpenBTree(eng)
			rng := rand.New(rand.NewPCG(11, 23))
			values := [][]byte{[]byte("short"), incompressibleBytes(1200), incompressibleBytes(3000), incompressibleBytes(6000)}
			want := make(map[string][]byte)
			for round := range 8 {
				commitTreeMutation(t, eng, func() error {
					for _, i := range rng.Perm(300) {
						key := fmt.Sprintf("%03d/%s", i, string(bytes.Repeat([]byte("x"), i%220)))
						if (i+round)%3 == 0 {
							if err := tree.Delete([]byte(key)); err != nil {
								return fmt.Errorf("delete %d: %w", i, err)
							}
							delete(want, key)
							continue
						}
						value := values[rng.IntN(len(values))]
						if err := tree.Put([]byte(key), value); err != nil {
							return fmt.Errorf("put %d: %w", i, err)
						}
						want[key] = value
					}
					return nil
				})
				verifyTreeModel(t, tree, want)
			}
			if err := eng.Close(); err != nil {
				t.Fatal(err)
			}
			eng, err = Open(path, opts)
			if err != nil {
				t.Fatal(err)
			}
			verifyTreeModel(t, OpenBTree(eng), want)
		})
	}
}

func verifyTreeModel(t *testing.T, tree *BTree, want map[string][]byte) {
	t.Helper()
	if count := validateTree(t, tree); count != len(want) {
		t.Fatalf("tree keys = %d, want %d", count, len(want))
	}
	for key, value := range want {
		got, found, err := tree.Get([]byte(key))
		if err != nil || !found || !bytes.Equal(got, value) {
			t.Fatalf("Get(%q): found=%v value length=%d, want %d, err=%v", key, found, len(got), len(value), err)
		}
	}
	count := 0
	var last []byte
	if err := tree.AscendRange(nil, nil, func(key, value []byte) bool {
		expected, ok := want[string(key)]
		if !ok || (last != nil && bytes.Compare(last, key) >= 0) || !bytes.Equal(value, expected) {
			t.Fatalf("invalid scan row %q after %q", key, last)
		}
		last = bytes.Clone(key)
		count++
		return count <= len(want)
	}); err != nil {
		t.Fatal(err)
	}
	if count != len(want) {
		t.Fatalf("range count = %d, want %d", count, len(want))
	}
	count, last = 0, nil
	if err := tree.DescendRange(nil, nil, func(key, value []byte) bool {
		expected, ok := want[string(key)]
		if !ok || (last != nil && bytes.Compare(last, key) <= 0) || !bytes.Equal(value, expected) {
			t.Fatalf("invalid reverse scan row %q after %q", key, last)
		}
		last = bytes.Clone(key)
		count++
		return count <= len(want)
	}); err != nil {
		t.Fatal(err)
	}
	if count != len(want) {
		t.Fatalf("reverse range count = %d, want %d", count, len(want))
	}
}
