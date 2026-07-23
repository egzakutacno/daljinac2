package mcp

import (
	"github.com/mark3labs/mcp-go/server"
)

func NewDMCPServer(version string) *server.MCPServer {
	s := server.NewMCPServer(
		"Daljinac2 Remote Agent",
		version,
		server.WithToolCapabilities(false),
		server.WithRecovery(),
	)

	registerTools(s)
	return s
}

func NewStreamableHTTPServer(mcpServer *server.MCPServer, path string) *server.StreamableHTTPServer {
	httpServer := server.NewStreamableHTTPServer(mcpServer,
		server.WithEndpointPath(path),
		server.WithDisableLocalhostProtection(true),
	)
	return httpServer
}

func registerTools(s *server.MCPServer) {
	registerScreenTools(s)
	registerInputTools(s)
	registerShellTools(s)
	registerFileTools(s)
	registerSystemTools(s)
	registerInfoTools(s)
}
