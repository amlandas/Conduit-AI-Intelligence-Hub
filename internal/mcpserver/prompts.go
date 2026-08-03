package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// PromptContext is the name of the KB context prompt.
const PromptContext = "kb_context"

// registerPrompts registers the kb_context prompt.
//
// The prompts/list shape (name, description, argument) is carried over
// unchanged from the previous server. That server, however, never implemented
// prompts/get -- a client that listed the prompt and then fetched it got an
// "unknown method: prompts/get" error. The handler below fixes that by
// returning the same prompt-ready context kb_search_with_context produces.
func (s *Server) registerPrompts() {
	s.mcp.AddPrompt(&mcp.Prompt{
		Name:        PromptContext,
		Description: "Get relevant context from knowledge base for a topic",
		Arguments: []*mcp.PromptArgument{
			{
				Name:        "topic",
				Description: "The topic to get context for",
				Required:    true,
			},
		},
	}, s.getContextPrompt)
}

// getContextPrompt handles prompts/get for kb_context.
func (s *Server) getContextPrompt(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	topic := req.Params.Arguments["topic"]
	if topic == "" {
		return nil, fmt.Errorf("missing required argument: topic")
	}

	// Reuse the kb_search_with_context handler so the prompt body and the tool
	// output can never drift apart.
	res, _, err := s.toolSearchWithContext(ctx, nil, searchWithContextArgs{Query: topic})
	if err != nil {
		return nil, err
	}

	text := ""
	if len(res.Content) > 0 {
		if tc, ok := res.Content[0].(*mcp.TextContent); ok {
			text = tc.Text
		}
	}

	return &mcp.GetPromptResult{
		Description: "Relevant knowledge base context for: " + topic,
		Messages: []*mcp.PromptMessage{
			{
				Role:    "user",
				Content: &mcp.TextContent{Text: text},
			},
		},
	}, nil
}
