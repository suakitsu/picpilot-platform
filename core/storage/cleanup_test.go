package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanupStaysInsideRootAndPaginates(t *testing.T) {
	dir := t.TempDir()
	s, err := NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	old := time.Now().Add(-72 * time.Hour)
	before := time.Now().Add(-48 * time.Hour)
	keys := []string{"aa/asset_00000000000000000000000000000001.png", "aa/asset_00000000000000000000000000000002.png", "tmp_00000000000000000000000000000003"}
	if err := os.MkdirAll(filepath.Join(dir, "aa"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		name := filepath.Join(dir, filepath.FromSlash(key))
		if err := os.WriteFile(name, []byte("test-only orphan"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(name, old, old); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "unrecognized.txt"), []byte("retain"), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := s.CleanupCandidates(before, "", 1)
	if err != nil || len(first) != 1 || first[0] != keys[0] {
		t.Fatal("first page", err, first)
	}
	second, err := s.CleanupCandidates(before, first[0], 10)
	if err != nil || len(second) != 2 {
		t.Fatal("second page", err, second)
	}
	if err := s.RemoveCleanupCandidate("../outside", before); err == nil {
		t.Fatal("unsafe path accepted")
	}
	if err := s.RemoveCleanupCandidate("unrecognized.txt", before); err == nil {
		t.Fatal("unrecognized file deleted")
	}
	if err := os.Chtimes(filepath.Join(dir, filepath.FromSlash(keys[0])), time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveCleanupCandidate(keys[0], before); err == nil {
		t.Fatal("newly modified file deleted")
	}
	if err := s.RemoveCleanupCandidate(keys[1], before); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "unrecognized.txt")); err != nil {
		t.Fatal("unrelated file lost")
	}
}
