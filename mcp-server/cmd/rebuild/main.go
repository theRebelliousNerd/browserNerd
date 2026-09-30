// Command rebuild compiles bin/browsernerd.exe (bin/browsernerd elsewhere)
// stamped with its version, beside a server that may be running it.
//
// A running MCP server holds its binary open. Windows refuses to overwrite a
// running executable but allows renaming it, so the current binary is moved
// aside to bin/browsernerd.<timestamp>.old(.exe), the new build takes its
// place, and the next server spawn (a /mcp reconnect) runs it. Old binaries no
// longer running are deleted on the way; a failed build puts the previous
// binary back so bin/ is never left without a server.
//
//	cd dev_tools/BrowserNERD/mcp-server && go run ./cmd/rebuild
//	go run ./cmd/rebuild -version 1.3.0
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func main() {
	version := flag.String("version", "1.2.1", "version stamped into the binary")
	flag.Parse()
	if err := build(*version); err != nil {
		fmt.Fprintln(os.Stderr, "build:", err)
		os.Exit(1)
	}
}

func build(version string) error {
	if _, err := os.Stat("go.mod"); err != nil {
		return fmt.Errorf("run from dev_tools/BrowserNERD/mcp-server (no go.mod here)")
	}
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	bin := filepath.Join("bin", "browsernerd"+ext)
	if err := os.MkdirAll("bin", 0o755); err != nil {
		return err
	}

	old, _ := filepath.Glob(filepath.Join("bin", "browsernerd.*.old"+ext))
	for _, path := range old {
		_ = os.Remove(path) // a binary still running stays; it is removed next time
	}

	previous := ""
	if _, err := os.Stat(bin); err == nil {
		previous = filepath.Join("bin", fmt.Sprintf("browsernerd.%s.old%s", time.Now().Format("20060102150405"), ext))
		if err := os.Rename(bin, previous); err != nil {
			return fmt.Errorf("move the current binary aside: %w", err)
		}
	}

	cmd := exec.Command("go", "build",
		"-ldflags", "-X browsernerd-mcp-server/internal/buildinfo.Version="+version,
		"-o", bin, "./cmd/server")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if previous != "" {
			_ = os.Rename(previous, bin)
		}
		return fmt.Errorf("go build failed; the previous binary is back in place: %w", err)
	}

	out, err := exec.Command(bin, "-version").Output()
	if err != nil {
		return fmt.Errorf("the new binary does not run: %w", err)
	}
	fmt.Print(strings.TrimSpace(string(out)) + "\n")
	fmt.Printf("Built %s. Reconnect the MCP server (/mcp) to run it.\n", bin)
	return nil
}
