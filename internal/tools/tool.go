package tools

import (
	"context"
	"encoding/json"
)

// PermLevel indicates the danger level of a tool operation.
type PermLevel int

const (
	PermNone    PermLevel = iota // No permission needed (e.g., listing models)
	PermRead                    // Read-only filesystem access
	PermWrite                   // Write filesystem access
	PermExecute                 // Execute subprocesses
	PermDanger                  // Dangerous operations (delete, push, etc.)
)

func (p PermLevel) String() string {
	switch p {
	case PermNone:
		return "none"
	case PermRead:
		return "read"
	case PermWrite:
		return "write"
	case PermExecute:
		return "execute"
	case PermDanger:
		return "dangerous"
	default:
		return "unknown"
	}
}

// Tool is the interface every tool implements. Tools are self-describing
// (the LLM reads the schema to know how to call them) and self-permissioning.
type Tool interface {
	// Name returns the tool's identifier (e.g., "read_file", "bash").
	Name() string

	// Description returns a human-readable description for the LLM.
	Description() string

	// InputSchema returns the JSON Schema describing the tool's input parameters.
	InputSchema() json.RawMessage

	// Execute runs the tool with the given input and returns the result.
	Execute(ctx context.Context, input json.RawMessage) (string, error)

	// PermissionLevel returns the tool's danger level.
	PermissionLevel() PermLevel
}
