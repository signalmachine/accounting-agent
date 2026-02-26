package ai

import (
	"context"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/responses"
)

// ToolHandler is the execution function for a read tool.
// It receives parsed JSON parameters and returns a JSON-encoded result string.
// Write tools do not have handlers — they are proposed to the user for confirmation.
type ToolHandler func(ctx context.Context, params map[string]any) (string, error)

// ToolDefinition describes a single tool in the registry.
// Read tools execute autonomously during the agentic loop.
// Write tools terminate the loop and surface a proposed action for human confirmation.
type ToolDefinition struct {
	Name        string
	Description string
	InputSchema map[string]any // JSON Schema for the tool's input parameters
	IsReadTool  bool           // true = execute autonomously; false = requires human confirmation
	Handler     ToolHandler    // non-nil for read tools only; nil for write tools
}

// ToolRegistry holds all tools available to the agent for a given call.
// Tools are registered by ApplicationService when building context for InterpretDomainAction.
// The registry is MCP-compatible: ToOpenAITools() and ToMCPTools() produce identical
// underlying definitions in their respective wire formats.
type ToolRegistry struct {
	tools []ToolDefinition
}

// NewToolRegistry creates an empty ToolRegistry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{}
}

// Register adds a tool to the registry.
func (r *ToolRegistry) Register(t ToolDefinition) {
	r.tools = append(r.tools, t)
}

// Get returns the ToolDefinition for a given tool name, and whether it was found.
func (r *ToolRegistry) Get(name string) (ToolDefinition, bool) {
	for _, t := range r.tools {
		if t.Name == name {
			return t, true
		}
	}
	return ToolDefinition{}, false
}

// All returns all registered tools.
func (r *ToolRegistry) All() []ToolDefinition {
	return r.tools
}

// ToOpenAITools converts the registry to the OpenAI Responses API tool format.
// Both read and write tools are included — the agent calls read tools autonomously
// and proposes write tools for human confirmation; this distinction is enforced in
// the agentic loop, not in the API payload.
func (r *ToolRegistry) ToOpenAITools() []responses.ToolUnionParam {
	out := make([]responses.ToolUnionParam, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, responses.ToolUnionParam{
			OfFunction: &responses.FunctionToolParam{
				Name:        t.Name,
				Description: openai.String(t.Description),
				Parameters:  t.InputSchema,
			},
		})
	}
	return out
}
