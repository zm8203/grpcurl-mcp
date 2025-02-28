package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// NewGrpcReflectionServer creates a new GrpcReflectionServer for the given target address (e.g., "localhost:8010").
func NewGrpcReflectionServer(host string) *GrpcReflectionServer {
	srv := server.NewMCPServer(
		"grpcReflectionServer",
		"1.0.0",
		server.WithLogging(),
	)

	grs := &GrpcReflectionServer{
		srv:  srv,
		host: host,
	}

	grs.registerTools()
	return grs
}

// Serve starts the MCP server over standard I/O.
func (g *GrpcReflectionServer) Serve() error {
	return server.ServeStdio(g.srv)
}

// registerTools registers the grpcurl-based tools available via the MCP server.
func (g *GrpcReflectionServer) registerTools() {
	// Tool 1: invoke
	// This tool invokes a gRPC method using reflection.
	// Parameters:
	//   - "method": Fully-qualified method name in slash notation (e.g., "package.Service/Method").
	//   - "request": JSON request payload.
	//   - "headers": (Optional) JSON object for custom gRPC metadata headers.
	invokeTool := mcp.NewTool(
		"invoke",
		mcp.WithDescription(`Invokes a gRPC method using reflection.
Parameters:
 - "method": Fully-qualified method name (e.g., package.Service/Method). (Use slash notation to invoke.)
 - "request": JSON payload for the request.
 - "headers": (Optional) JSON object for custom gRPC headers, e.g. {"Authorization": "Bearer <token>"}.`),
		mcp.WithString("method", mcp.Description("Fully-qualified method name (e.g., package.Service/Method)"), mcp.Required()),
		mcp.WithString("request", mcp.Description("JSON request payload"), mcp.Required()),
		mcp.WithString("headers", mcp.Description("Optional JSON object for custom gRPC headers")),
	)
	g.srv.AddTool(invokeTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.Params.Arguments

		method, _ := args["method"].(string)
		reqPayload, _ := args["request"].(string)
		headersJSON, _ := args["headers"].(string)

		// Build grpcurl arguments.
		grpcArgs := []string{}

		// Process custom headers if provided.
		if headersJSON != "" {
			meta := map[string]string{}
			if err := json.Unmarshal([]byte(headersJSON), &meta); err != nil {
				return toolError("Failed to parse headers JSON: " + err.Error()), nil
			}
			for k, v := range meta {
				grpcArgs = append(grpcArgs, "-H", fmt.Sprintf("%s: %s", k, v))
			}
		}

		// Append the JSON request payload.
		grpcArgs = append(grpcArgs, "-d", reqPayload)

		// Execute the grpcurl command.
		out, err := runGrpcurl(ctx, g.host, method, grpcArgs...)
		if err != nil {
			return toolError(err.Error()), nil
		}
		return toolSuccess(out), nil
	})

	// Tool 2: list
	// This tool lists all available gRPC services on the target server using reflection.
	listTool := mcp.NewTool(
		"list",
		mcp.WithDescription("Lists all available gRPC services on the target server using reflection."),
	)
	g.srv.AddTool(listTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		out, err := runGrpcurl(ctx, g.host, "list")
		if err != nil {
			return toolError(err.Error()), nil
		}
		return toolSuccess(out), nil
	})

	// Tool 3: describe
	// This tool describes a gRPC service or message type.
	// Note: The grpcurl "describe" command works with symbols in dot notation.
	// For example, to describe a service, use "mypackage.MyService" (not "mypackage.MyService/MyMethod").
	// To see method details, describe the service and then inspect the request/response message types.
	describeTool := mcp.NewTool(
		"describe",
		mcp.WithDescription(`Describes a gRPC service or message type.
Provide the target entity using dot notation.
Examples:
 - "mypackage.MyService" to describe the service.
 - "mypackage.MyMessage.MyRpc" to describe a specific RPC method.
 - "mypackage.MyMessage" to describe a message type.
Note: Slash notation (e.g., "mypackage.MyService/MyMethod") is used for invoking RPCs, not for describing symbols.`),
		WithStringArray("entities", mcp.Description("The services or messages type to describe (use dot notation)"), mcp.Required()),
	)
	g.srv.AddTool(describeTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.Params.Arguments
		entities, _ := args["entities"].([]interface{})
		outs := []string{}
		for _, entity := range entities {
			entityStr, ok := entity.(string)
			if !ok {
				return toolError(fmt.Sprintf("entity '%v' is not a string", entity)), nil
			}
			out, err := runGrpcurl(ctx, g.host, "describe", entityStr)
			if err != nil {
				return toolError(err.Error()), nil
			}
			outs = append(outs, entityStr, out)
		}

		return toolSuccess(strings.Join(outs, "\n")), nil
	})
}

// runGrpcurl executes the grpcurl command with the provided arguments.
// The command structure is:
//
//	grpcurl -plaintext <host> <subcommand> [additional arguments...]
func runGrpcurl(ctx context.Context, host, subcmd string, additionalArgs ...string) (string, error) {
	args := append(additionalArgs, "-plaintext", host, subcmd)
	cmd := exec.CommandContext(ctx, "grpcurl", args...)

	// Capture stderr for error details.
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	// Execute the command and capture stdout.
	stdout, err := cmd.Output()
	if err != nil {
		stderrBytes, _ := io.ReadAll(stderrPipe)
		fakeCmd := fmt.Sprintf("grpcurl %s", strings.Join(args, " "))
		return "", fmt.Errorf("grpcurl command (%s) failed: %w\nstdout: %s\nstderr: %s", fakeCmd, err, string(stdout), string(stderrBytes))
	}
	return string(stdout), nil
}

func main() {

	grpcServer := NewGrpcReflectionServer(os.Getenv("ADDRESS"))
	if err := grpcServer.Serve(); err != nil && err != io.EOF {
		log.Fatal("Error serving MCP server:", err)
		os.Exit(1)
	}
}

// WithStringArray adds a string array property to the tool schema.
// It accepts property options to configure the string array property's behavior and constraints.
func WithStringArray(name string, opts ...mcp.PropertyOption) mcp.ToolOption {
	return func(t *mcp.Tool) {
		schema := map[string]interface{}{
			"type": "array",
			"items": map[string]interface{}{
				"type": "string",
			},
		}

		for _, opt := range opts {
			opt(schema)
		}

		// Remove required from property schema and add to InputSchema.required
		if required, ok := schema["required"].(bool); ok && required {
			delete(schema, "required")
			if t.InputSchema.Required == nil {
				t.InputSchema.Required = []string{name}
			} else {
				t.InputSchema.Required = append(t.InputSchema.Required, name)
			}
		}

		t.InputSchema.Properties[name] = schema
	}
}

// toolSuccess creates a successful MCP response with the provided text contents.
func toolSuccess(contents ...string) *mcp.CallToolResult {
	var iface []interface{}
	for _, c := range contents {
		iface = append(iface, mcp.NewTextContent(c))
	}
	return &mcp.CallToolResult{
		Content: iface,
		IsError: false,
	}
}

// toolError creates an MCP error response with the given error message.
func toolError(message string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []interface{}{mcp.NewTextContent(message)},
		IsError: true,
	}
}

// GrpcReflectionServer wraps grpcurl functionalities into an MCP server.
type GrpcReflectionServer struct {
	srv  *server.MCPServer
	host string
}
