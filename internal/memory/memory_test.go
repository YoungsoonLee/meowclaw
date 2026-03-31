package memory

import (
	"context"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := New(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestSaveAndLoadHistory(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := store.Save(ctx, "sess-1", "user", "hello"); err != nil {
		t.Fatalf("save user msg: %v", err)
	}
	if err := store.Save(ctx, "sess-1", "assistant", "hi there"); err != nil {
		t.Fatalf("save assistant msg: %v", err)
	}
	if err := store.Save(ctx, "sess-1", "user", "how are you?"); err != nil {
		t.Fatalf("save user msg 2: %v", err)
	}

	msgs, err := store.LoadHistory(ctx, "sess-1", 10)
	if err != nil {
		t.Fatalf("load history: %v", err)
	}

	if len(msgs) != 3 {
		t.Fatalf("history length = %d, want 3", len(msgs))
	}

	// should be in chronological order
	if msgs[0].Content != "hello" {
		t.Errorf("msgs[0] = %q, want hello", msgs[0].Content)
	}
	if msgs[1].Role != "assistant" {
		t.Errorf("msgs[1].Role = %q, want assistant", msgs[1].Role)
	}
	if msgs[2].Content != "how are you?" {
		t.Errorf("msgs[2] = %q, want 'how are you?'", msgs[2].Content)
	}
}

func TestLoadHistoryLimit(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		store.Save(ctx, "sess-limit", "user", "msg")
	}

	msgs, err := store.LoadHistory(ctx, "sess-limit", 3)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(msgs) != 3 {
		t.Errorf("got %d messages, want 3", len(msgs))
	}
}

func TestLoadHistoryEmptySession(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	msgs, err := store.LoadHistory(ctx, "nonexistent", 10)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected empty, got %d", len(msgs))
	}
}

func TestSessionIsolation(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	store.Save(ctx, "sess-a", "user", "message A")
	store.Save(ctx, "sess-b", "user", "message B")

	msgsA, _ := store.LoadHistory(ctx, "sess-a", 10)
	msgsB, _ := store.LoadHistory(ctx, "sess-b", 10)

	if len(msgsA) != 1 || msgsA[0].Content != "message A" {
		t.Errorf("session A contaminated: %v", msgsA)
	}
	if len(msgsB) != 1 || msgsB[0].Content != "message B" {
		t.Errorf("session B contaminated: %v", msgsB)
	}
}

func TestSearch(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	store.Save(ctx, "s1", "user", "the quick brown fox jumps over the lazy dog")
	store.Save(ctx, "s1", "assistant", "that is a pangram")
	store.Save(ctx, "s2", "user", "hello world")

	results, err := store.Search(ctx, "fox", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 search result for 'fox'")
	}

	found := false
	for _, r := range results {
		if r == "the quick brown fox jumps over the lazy dog" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected to find fox sentence, got: %v", results)
	}
}

func TestListSessions(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	store.Save(ctx, "sess-x", "user", "msg1")
	store.Save(ctx, "sess-y", "user", "msg2")
	store.Save(ctx, "sess-y", "assistant", "msg3")

	sessions, err := store.ListSessions(ctx)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("got %d sessions, want 2", len(sessions))
	}

	counts := map[string]int{}
	for _, s := range sessions {
		counts[s.ID] = s.MsgCount
	}
	if counts["sess-x"] != 1 {
		t.Errorf("sess-x msg count = %d, want 1", counts["sess-x"])
	}
	if counts["sess-y"] != 2 {
		t.Errorf("sess-y msg count = %d, want 2", counts["sess-y"])
	}
}
