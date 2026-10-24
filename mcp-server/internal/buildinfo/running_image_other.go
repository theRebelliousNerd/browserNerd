//go:build !linux

package buildinfo

// A logical executable pathname can resolve to replacement bytes. Until a
// native held-image reader exists, identity remains explicitly unavailable.
func readRunningImage() runningImageIdentity {
	return runningImageIdentity{source: "unavailable", reason: "running_image_reader_unavailable_on_platform"}
}
