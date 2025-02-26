package main

import (
	"context"
	"io"
	"log"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection/grpc_reflection_v1alpha"

	"github.com/fullstorydev/grpcurl"
	"github.com/fullstorydev/grpcurl/formatter"
)

// toolSuccess constructs a successful MCP response.
func toolSuccess(contents []mcp.TextContent) *mcp.CallToolResult {
	var iface []interface{}
	for _, c := range contents {
		iface = append(iface, c)
	}
	return &mcp.CallToolResult{Content: iface, IsError: false}
}

// toolError constructs an MCP error response.
func toolError(message string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []interface{}{mcp.NewTextContent(message)}, IsError: true}
}

// GrpcReflectionServer encapsulates our new MCP server.
// The target gRPC address is provided at initialization.
type GrpcReflectionServer struct {
	srv    *server.MCPServer
	target string
}

// NewGrpcReflectionServer creates and configures a new MCP server instance with a fixed target address.
func NewGrpcReflectionServer(target string) *GrpcReflectionServer {
	srv := server.NewMCPServer("grpcReflectionServer", "1.0.0", server.WithLogging())
	gr := &GrpcReflectionServer{
		srv:    srv,
		target: target,
	}
	gr.registerGrpcReflectionTool()
	return gr
}

// registerGrpcReflectionTool registers a tool that uses reflection to invoke a gRPC method.
func (gr *GrpcReflectionServer) registerGrpcReflectionTool() {
	tool := mcp.NewTool("executeGrpcReflectionQuery",
		mcp.WithDescription("Invokes a gRPC method using reflection (like grpcurl). The target is preconfigured."),
		mcp.WithString("method", mcp.Description("Fully-qualified method name in the format Package.Service/Method"), mcp.Required()),
		mcp.WithString("request", mcp.Description("JSON request payload"), mcp.Required()),
	)
	gr.srv.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.Params.Arguments
		method, _ := args["method"].(string)
		reqPayload, _ := args["request"].(string)

		// Use the preconfigured target address.
		conn, err := grpc.Dial(gr.target, grpc.WithInsecure())
		if err != nil {
			return toolError("Failed to connect to gRPC server: " + err.Error()), nil
		}
		defer conn.Close()

		// Create a reflection client.
		refClient := grpc_reflection_v1alpha.NewServerReflectionClient(conn)
		descSource, err := grpcurl.DescriptorSourceFromServer(ctx, refClient)
		if err != nil {
			return toolError("Error creating descriptor source: " + err.Error()), nil
		}

		var responseBuilder strings.Builder
		// Create a JSON formatter for the response.
		f, err := formatter.NewFormatter("json", descSource, false, false)
		if err != nil {
			return toolError("Error setting up formatter: " + err.Error()), nil
		}

		// Invoke the RPC using grpcurl.
		err = grpcurl.InvokeRPC(ctx, descSource, conn, method, []string{"-d", reqPayload}, grpcurl.HandlerFunc(func(m interface{}) error {
			str, err := f.Format(m)
			if err != nil {
				return err
			}
			responseBuilder.WriteString(str)
			responseBuilder.WriteString("\n")
			return nil
		}))
		if err != nil {
			return toolError("RPC invocation failed: " + err.Error()), nil
		}

		return toolSuccess([]mcp.TextContent{
			{Type: "text", Text: responseBuilder.String()},
		}), nil
	})
}

// Serve starts the MCP server over standard I/O.
func (gr *GrpcReflectionServer) Serve() error {
	log.Println("Starting GrpcReflectionServer over stdio")
	return server.ServeStdio(gr.srv)
}

func main() {
	// Specify the target gRPC server address here.
	targetAddress := "localhost:8010"
	grpcServer := NewGrpcReflectionServer(targetAddress)

	// Serve the MCP server over standard I/O.
	if err := grpcServer.Serve(); err != nil && err != io.EOF {
		log.Fatal("Error serving MCP server:", err)
		os.Exit(1)
	}
}
