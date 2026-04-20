package live

import (
	"regexp"
	"time"

	"github.com/Mizune/mosaic/internal/store"
	"github.com/Mizune/mosaic/pkg/transcript"
)

// agentMetadata is everything the reconciler needs to show a
// sub-agent row with a useful label. It is extracted from the parent
// transcript's `Agent` tool_use input once the matching tool_result
// has told us the agent_id the two halves refer to.
type agentMetadata struct {
	Description     string
	SubagentType    string
	RunInBackground bool
}

// pendingAgentTool is half of an Agent tool_use/tool_result pair: the
// tool_use has been parsed, but the tool_result (which carries the
// agent_id) has not yet arrived. Once it does, the reconciler moves
// the entry out of this map and into agentMeta, keyed by the
// resolved agent_id.
type pendingAgentTool struct {
	ClaudeSessionID string
	Input           transcript.SubAgentInput
	CreatedAt       time.Time
}

// pendingToolExpiry is the maximum age a pending tool entry can reach
// before it is considered stale and pruned. This prevents the queue
// from growing unboundedly when an Agent tool_use never receives a
// matching tool_result (e.g. the agent crashed before writing any
// transcript).
const pendingToolExpiry = time.Minute

// agentIDPattern captures the "agentId: <hex>" marker Claude Code
// embeds in the tool_result text for a successful Agent launch. The
// id is a lowercase hex string whose length is not fixed, so we
// match a permissive character set.
var agentIDPattern = regexp.MustCompile(`agentId:\s*([a-zA-Z0-9]+)`)

// extractAgentID pulls the agent_id out of a tool_result's content
// bytes. The content is either a plain string or a JSON array of
// text blocks; we just regex over the raw bytes so both shapes work
// without dedicated decoding.
func extractAgentID(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	m := agentIDPattern.FindSubmatch(raw)
	if len(m) < 2 {
		return ""
	}
	return string(m[1])
}

// defaultSubagentType returns the value that goes into the
// subagents.subagent_type column when Claude does not specify one
// explicitly. Using store.DefaultSubagentType matches Claude Code's
// own default so the TUI row does not show "" when metadata is
// missing.
func defaultSubagentType(s string) string {
	if s == "" {
		return store.DefaultSubagentType
	}
	return s
}
