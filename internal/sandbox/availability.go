package sandbox

import "os/exec"

// DockerAvailable reports whether the `docker` binary can be found
// on $PATH. The wizard uses it to gray out the Docker option when
// the user has no Docker installation, turning the missing tool
// into a clear message at the selection site instead of a cryptic
// ENOENT at session spawn time.
func DockerAvailable() bool {
	_, err := exec.LookPath("docker")
	return err == nil
}
