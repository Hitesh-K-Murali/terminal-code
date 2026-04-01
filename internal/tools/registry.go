package tools

import (
	"encoding/json"
	"sync"

	"github.com/Hitesh-K-Murali/terminal-code/internal/memory"
	"github.com/Hitesh-K-Murali/terminal-code/internal/sandbox"
)

// Registry holds all registered tools and provides lookup/dispatch.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

// Register adds a tool to the registry.
func (r *Registry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Name()] = t
}

// Get returns a tool by name.
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// All returns all registered tools.
func (r *Registry) All() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}

// ToolDefinitions returns the tool definitions in the format expected
// by LLM provider APIs (Anthropic tool_use format).
func (r *Registry) ToolDefinitions() json.RawMessage {
	r.mu.RLock()
	defer r.mu.RUnlock()

	type toolDef struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"input_schema"`
	}

	defs := make([]toolDef, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, toolDef{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.InputSchema(),
		})
	}

	data, _ := json.Marshal(defs)
	return data
}

// RegisterDefaults registers all built-in tools with the appropriate
// sandbox components for enforcement.
func RegisterDefaults(
	reg *Registry,
	pathChecker *sandbox.PathChecker,
	runner *sandbox.IsolatedRunner,
	auditLog *sandbox.AuditLog,
	plan *sandbox.EnforcementPlan,
	dirCache *memory.DirCache,
) {
	reg.Register(NewReadFileTool(pathChecker, auditLog))
	reg.Register(NewWriteFileTool(pathChecker, auditLog, plan))
	reg.Register(NewGlobTool())
	reg.Register(NewGrepTool())
	reg.Register(NewBashTool(runner, auditLog, plan))
	reg.Register(NewGitTool())
	if dirCache != nil {
		reg.Register(NewDirContextTool(dirCache))
	}

	// Tool count reported by app startup — no output here
}
