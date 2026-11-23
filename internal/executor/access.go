package executor

// Permission probes run as another account (sudo -u <user> -- ...).
//
// They must ask the kernel. Ubuntu 26.04 ships the Rust coreutils, whose
// test -r/-x only compare the mode bits with the user's primary group and
// ignore ACLs and supplementary groups, so "/usr/bin/test -x dir" reports
// "denied" for an account that was granted access through an ACL or a
// site group. chdir(2) and open(2) apply the real checks. The path is a
// positional parameter, never part of the shell text.

// CanEnterArgs returns sudo arguments that exit 0 when user can traverse dir.
func CanEnterArgs(user, dir string) []string {
	return []string{"-u", user, "--", "/bin/sh", "-c", `cd -- "$1"`, "sh", dir}
}

// CanReadArgs returns sudo arguments that exit 0 when user can open path for
// reading.
func CanReadArgs(user, path string) []string {
	return []string{"-u", user, "--", "/bin/sh", "-c", `: < "$1"`, "sh", path}
}
