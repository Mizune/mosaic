package store_test

import (
	"context"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Mizune/mosaic/internal/store"
)

var _ = Describe("Migrate", func() {
	var (
		ctx context.Context
		s   *store.Store
	)

	BeforeEach(func() {
		ctx = context.Background()
		dir := GinkgoT().TempDir()
		var err error
		s, err = store.Open(filepath.Join(dir, "state.db"))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(s.Close)
	})

	It("applies migration 0001 to a fresh database", func() {
		Expect(s.Migrate(ctx)).To(Succeed())

		applied, err := s.AppliedVersions(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(applied).To(HaveKey(1))
	})

	It("is idempotent on a second run", func() {
		Expect(s.Migrate(ctx)).To(Succeed())
		Expect(s.Migrate(ctx)).To(Succeed())

		var count int
		Expect(s.DB().GetContext(ctx, &count,
			`SELECT count(*) FROM schema_migrations WHERE version = 1`)).To(Succeed())
		Expect(count).To(Equal(1))
	})

	It("creates the sessions table after migrating", func() {
		Expect(s.Migrate(ctx)).To(Succeed())

		var name string
		Expect(s.DB().GetContext(ctx, &name,
			`SELECT name FROM sqlite_master WHERE type='table' AND name='sessions'`)).To(Succeed())
		Expect(name).To(Equal("sessions"))
	})

	It("backfills generic agent session columns from Claude columns", func() {
		_, err := s.DB().ExecContext(ctx, `
CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE sessions (
    id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE, project_path TEXT NOT NULL,
    worktree_path TEXT NOT NULL, branch TEXT, tmux_session TEXT NOT NULL UNIQUE,
    tool TEXT NOT NULL DEFAULT 'claude', status TEXT NOT NULL DEFAULT 'idle',
    claude_session_id TEXT, conductor_id TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_active_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    attention_state TEXT NOT NULL DEFAULT 'none', attention_title TEXT,
    attention_body TEXT, attention_since DATETIME, attention_source TEXT,
    sandbox_mode TEXT NOT NULL DEFAULT 'none', sandbox_container TEXT NOT NULL DEFAULT ''
);
CREATE TABLE subagents (
    id TEXT PRIMARY KEY, parent_session_id TEXT NOT NULL,
    claude_session_id TEXT NOT NULL, agent_id TEXT NOT NULL,
    subagent_type TEXT NOT NULL, description TEXT, status TEXT NOT NULL DEFAULT 'spawning',
    started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, completed_at DATETIME,
    transcript_path TEXT NOT NULL, current_tool TEXT, current_tool_started_at DATETIME,
    last_event_offset INTEGER NOT NULL DEFAULT 0, name TEXT
);
INSERT INTO sessions (id, name, project_path, worktree_path, tmux_session, claude_session_id)
VALUES ('s1', 'one', '/repo', '/repo', 'mos_s1', 'claude-1');
INSERT INTO subagents (id, parent_session_id, claude_session_id, agent_id, subagent_type, transcript_path)
VALUES ('sub1', 's1', 'claude-1', 'agent-1', 'general-purpose', '/tmp/sub.jsonl');
INSERT INTO schema_migrations(version) VALUES (1),(2),(3),(4),(5),(6),(7),(8);
`)
		Expect(err).NotTo(HaveOccurred())

		Expect(s.Migrate(ctx)).To(Succeed())

		var agentSessionID, source string
		Expect(s.DB().QueryRowxContext(ctx,
			`SELECT agent_session_id, agent_session_source FROM sessions WHERE id = 's1'`,
		).Scan(&agentSessionID, &source)).To(Succeed())
		Expect(agentSessionID).To(Equal("claude-1"))
		Expect(source).To(Equal("claude"))

		var subSessionID, runtime string
		Expect(s.DB().QueryRowxContext(ctx,
			`SELECT agent_session_id, agent_runtime FROM subagents WHERE id = 'sub1'`,
		).Scan(&subSessionID, &runtime)).To(Succeed())
		Expect(subSessionID).To(Equal("claude-1"))
		Expect(runtime).To(Equal("claude"))
	})
})
