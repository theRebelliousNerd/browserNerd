package mcp

import (
	"testing"
	"time"

	"browsernerd-mcp-server/internal/buildinfo"
)

func TestDoctorBinaryIdentityFieldsAndUnavailableTime(t *testing.T) {
	known := doctorBinaryIdentity(buildinfo.Info{Version: "1", Revision: "abcdef", Modified: true,
		BuildTime: time.Unix(100, 0), PID: 123, IdentityStatus: "verified", IdentitySource: "proc_self_exe", SHA256: "held-image-hash", FileID: "device:inode"})
	if known["built"] == nil || known["revision"] != "abcdef+dirty" || known["pid"] != 123 || known["sha256"] != "held-image-hash" || known["file_id"] != "device:inode" || known["identity_status"] != "verified" {
		t.Fatalf("held identity missing or existing fields changed: %+v", known)
	}
	unknown := doctorBinaryIdentity(buildinfo.Info{Version: "1", PID: 123, IdentityStatus: "unavailable", IdentitySource: "unavailable", IdentityError: "running_image_reader_unavailable_on_platform"})
	if unknown["built"] != nil || unknown["sha256"] != nil || unknown["file_id"] != nil || unknown["identity_status"] != "unavailable" || unknown["identity_error"] != "running_image_reader_unavailable_on_platform" {
		t.Fatalf("unknown image fabricated identity/time: %+v", unknown)
	}
}
