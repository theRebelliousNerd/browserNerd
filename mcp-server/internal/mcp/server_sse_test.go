package mcp

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"
)

// The SSE transport drives a browser and reads its facts, so by default it is
// reachable from this machine only. It once listened on every interface.
func TestSSEListensOnLoopbackByDefault(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Recorder.Enabled = false
	if host := cfg.MCP.GetSSEHost(); host != "127.0.0.1" {
		t.Fatalf("the default SSE host must be loopback, got %q", host)
	}
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
	go func() { done <- server.StartSSE(ctx, cfg.MCP.GetSSEHost(), port) }()
	defer func() {
		cancel()
		<-done
	}()

	loopback := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	deadline := time.Now().Add(3 * time.Second)
	for {
		conn, dialErr := net.DialTimeout("tcp", loopback, 200*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the SSE server never accepted on %s: %v", loopback, dialErr)
		}
		time.Sleep(20 * time.Millisecond)
	}

	external := nonLoopbackIPv4()
	if external == "" {
		t.Log("this machine has no non-loopback IPv4 address; only the loopback bind is asserted")
		return
	}
	target := net.JoinHostPort(external, strconv.Itoa(port))
	if conn, dialErr := net.DialTimeout("tcp", target, 500*time.Millisecond); dialErr == nil {
		_ = conn.Close()
		t.Fatalf("the SSE server answers on %s; it must listen on loopback only by default", target)
	}
}

func TestSSEAdvertisesAReachableHost(t *testing.T) {
	for host, want := range map[string]string{"127.0.0.1": "127.0.0.1", "0.0.0.0": "localhost", "::": "localhost", "10.0.0.5": "10.0.0.5"} {
		if got := sseAdvertisedHost(host); got != want {
			t.Fatalf("sseAdvertisedHost(%q) = %q, want %q", host, got, want)
		}
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
