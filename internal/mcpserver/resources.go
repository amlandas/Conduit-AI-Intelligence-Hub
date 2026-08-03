package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// resourceURIPrefix is the URI scheme+path prefix for KB source resources.
const resourceURIPrefix = "kb://source/"

// resourceURITemplate is the RFC 6570 template for KB source resources. The
// concrete URIs it expands to (kb://source/{sourceID}) are byte-identical to
// the ones the previous server emitted.
const resourceURITemplate = "kb://source/{sourceID}"

// registerResources wires up the kb://source/{sourceID} resource.
//
// The previous server answered resources/list dynamically from the source
// table. The SDK only lists statically registered resources, so the live
// listing is reproduced with a receiving middleware (see listResourcesMiddleware)
// while reads are served by a resource template.
func (s *Server) registerResources() {
	s.mcp.AddResourceTemplate(&mcp.ResourceTemplate{
		Name:        "kb-source",
		Description: "Knowledge base source metadata, addressed by source ID.",
		MIMEType:    "application/json",
		URITemplate: resourceURITemplate,
	}, s.readSourceResource)

	s.mcp.AddReceivingMiddleware(s.listResourcesMiddleware)
}

// readSourceResource reads a kb://source/{sourceID} resource.
func (s *Server) readSourceResource(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	uri := req.Params.URI

	if !strings.HasPrefix(uri, resourceURIPrefix) {
		return nil, fmt.Errorf("unknown resource URI: %s", uri)
	}

	sourceID := strings.TrimPrefix(uri, resourceURIPrefix)
	source, err := s.source.Get(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("get source: %w", err)
	}

	content, _ := json.MarshalIndent(source, "", "  ")

	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{
			{
				URI:      uri,
				MIMEType: "application/json",
				Text:     string(content),
			},
		},
	}, nil
}

// listResourcesMiddleware answers resources/list from the live source table,
// preserving the previous server's behavior. Every other method is passed
// through untouched.
func (s *Server) listResourcesMiddleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		if method != "resources/list" {
			return next(ctx, method, req)
		}

		// Errors are swallowed here exactly as before: an unreadable source
		// table yields an empty resource list rather than a protocol error.
		sources, _ := s.source.List(ctx)

		res := &mcp.ListResourcesResult{Resources: []*mcp.Resource{}}
		for _, src := range sources {
			res.Resources = append(res.Resources, &mcp.Resource{
				URI:         resourceURIPrefix + src.SourceID,
				Name:        src.Name,
				Description: fmt.Sprintf("Knowledge base source: %s (%d documents)", src.Path, src.DocCount),
				MIMEType:    "application/json",
			})
		}
		return res, nil
	}
}
