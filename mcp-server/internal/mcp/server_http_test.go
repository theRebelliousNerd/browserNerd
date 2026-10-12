package mcp

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"
)

// startHTTPForTest serves a progressive-only server over Streamable HTTP on a
// free loopback port and returns the port.
func startHTTPForTest(t *testing.T, cfg config.Config) int {
	t.Helper()
	engine, err := mangle.NewEngine(cfg.Mangle)
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(cfg, browser.NewSessionManager(cfg.Browser, engine), engine)
	if err != nil {
		t.Fatal(err)
	}
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	_ = probe.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.StartHTTP(ctx, cfg.MCP.GetHTTPHost(), port) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("StartHTTP: %v", err)
		}
	})

	loopback := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	deadline := time.Now().Add(3 * time.Second)
	for {
		conn, dialErr := net.DialTimeout("tcp", loopback, 200*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			return port
		}
		if time.Now().After(deadline) {
			t.Fatalf("the HTTP server never accepted on %s: %v", loopback, dialErr)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// The HTTP transport drives a browser and reads its facts, so by default it is
// reachable from this machine only.
func TestHTTPListensOnLoopbackByDefault(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Recorder.Enabled = false
	if host := cfg.MCP.GetHTTPHost(); host != "127.0.0.1" {
		t.Fatalf("the default HTTP host must be loopback, got %q", host)
	}
	port := startHTTPForTest(t, cfg)

	external := nonLoopbackIPv4()
	if external == "" {
		t.Log("this machine has no non-loopback IPv4 address; only the loopback bind is asserted")
		return
	}
	target := net.JoinHostPort(external, strconv.Itoa(port))
	if conn, dialErr := net.DialTimeout("tcp", target, 500*time.Millisecond); dialErr == nil {
		_ = conn.Close()
		t.Fatalf("the HTTP server answers on %s; it must listen on loopback only by default", target)
	}
}

// An MCP client speaking Streamable HTTP initializes and lists the tools.
func TestHTTPServesMCPAtEndpoint(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Recorder.Enabled = false
	port := startHTTPForTest(t, cfg)

	client, err := mcpclient.NewStreamableHttpClient("http://localhost:" + strconv.Itoa(port) + httpEndpointPath)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := client.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := client.Initialize(ctx, mcp.InitializeRequest{Params: mcp.InitializeParams{
		ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
		ClientInfo:      mcp.Implementation{Name: "http-test", Version: "1"},
	}}); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	tools, err := client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(tools.Tools) != 11 {
		t.Fatalf("listed %d tools over HTTP, want the 11 progressive tools", len(tools.Tools))
	}
}

func nonLoopbackIPv4() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() || ipNet.IP.To4() == nil || ipNet.IP.IsLinkLocalUnicast() {
			continue
		}
		return ipNet.IP.String()
	}
	return ""
}
