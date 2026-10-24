package buildinfo

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"crypto/sha256"
)

func TestRunningImageReplacementHelper(t *testing.T) {
	mode := os.Getenv("BROWSERNERD_RUNNING_IMAGE_HELPER")
	if mode == "" {
		return
	}
	reader := bufio.NewReader(os.Stdin)
	if mode == "late" {
		fmt.Println("ready")
	} else {
		if err := json.NewEncoder(os.Stdout).Encode(Read()); err != nil {
			panic(err)
		}
	}
	if _, err := reader.ReadString('\n'); err != nil {
		panic(err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(Read()); err != nil {
		panic(err)
	}
	os.Exit(0)
}

func TestRunningImageIdentitySurvivesPathReplacement(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux held-image adapter regression")
	}
	for _, mode := range []string{"late", "early"} {
		t.Run(mode, func(t *testing.T) {
			original, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			image := filepath.Join(t.TempDir(), "own-running-image")
			in, err := os.Open(original)
			if err != nil {
				t.Fatal(err)
			}
			out, err := os.OpenFile(image, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
			if err != nil {
				in.Close()
				t.Fatal(err)
			}
			hash := sha256.New()
			_, copyErr := io.Copy(io.MultiWriter(out, hash), in)
			in.Close()
			out.Close()
			if copyErr != nil {
				t.Fatal(copyErr)
			}
			expectedSHA := fmt.Sprintf("%x", hash.Sum(nil))
			oldTime := time.Now().Add(-2 * time.Hour).Truncate(time.Second)
			if err := os.Chtimes(image, oldTime, oldTime); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(image, "-test.run=^TestRunningImageReplacementHelper$")
			cmd.Env = append(os.Environ(), "BROWSERNERD_RUNNING_IMAGE_HELPER="+mode)
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			var stderr strings.Builder
			cmd.Stderr = &stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = stdin.Close()
				if cmd.ProcessState == nil {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
				}
			})
			reader := bufio.NewReader(stdout)
			first, err := reader.ReadString('\n')
			if err != nil {
				t.Fatalf("own helper handshake: %v %s", err, stderr.String())
			}
			if mode == "late" && strings.TrimSpace(first) != "ready" {
				t.Fatalf("helper read image before requested: %s", first)
			}
			replacement := image + ".candidate"
			if err := os.WriteFile(replacement, []byte("different installed bytes, not the executing image\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			future := oldTime.Add(4 * time.Hour)
			if err := os.Chtimes(replacement, future, future); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(replacement, image); err != nil {
				t.Fatal(err)
			}
			if _, err := io.WriteString(stdin, "read held image\n"); err != nil {
				t.Fatal(err)
			}
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatalf("own helper identity: %v %s", err, stderr.String())
			}
			if err := cmd.Wait(); err != nil {
				t.Fatalf("own helper: %v %s", err, stderr.String())
			}
			var info map[string]interface{}
			if err := json.Unmarshal([]byte(line), &info); err != nil {
				t.Fatal(err)
			}
			built, err := time.Parse(time.RFC3339, fmt.Sprint(info["build_time"]))
			if err != nil || !built.Equal(oldTime) {
				t.Fatalf("reported replacement time instead of held image: old=%s new=%s actual=%+v", oldTime, future, info)
			}
			if mode == "early" {
				var before map[string]interface{}
				if err := json.Unmarshal([]byte(first), &before); err != nil {
					t.Fatal(err)
				}
				if info["build_time"] != before["build_time"] || info["sha256"] != before["sha256"] || info["file_id"] != before["file_id"] {
					t.Fatalf("running identity changed across path replacement: before=%+v after=%+v", before, info)
				}
			}
			if info["sha256"] != expectedSHA || info["pid"] != float64(cmd.Process.Pid) || info["identity_status"] != "verified" || info["identity_source"] != "proc_self_exe" || info["file_id"] == "" {
				t.Fatalf("missing actual held identity: %+v want sha=%s pid=%d", info, expectedSHA, cmd.Process.Pid)
			}
			t.Logf("%s: held image sha=%s pid=%d stayed truthful after atomic replacement", mode, expectedSHA, cmd.Process.Pid)
		})
	}
}
