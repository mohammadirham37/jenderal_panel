package executor

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// Secrets must not travel in argv: every local account can read another
// process's command line through /proc. These helpers hand the value to the
// child through the environment instead, which is only readable by the
// process owner and root. sudo clears the environment by default, so the
// names are listed explicitly with --preserve-env.

// sudoEnvArgs returns the sudo prefix that keeps the named variables, and the
// environment to run sudo with.
func sudoEnvArgs(env map[string]string, name string, args []string) ([]string, []string) {
	names := make([]string, 0, len(env))
	for key := range env {
		names = append(names, key)
	}
	sort.Strings(names)

	childEnv := os.Environ()
	for _, key := range names {
		childEnv = append(childEnv, key+"="+env[key])
	}

	sudoArgs := make([]string, 0, len(args)+2)
	if len(names) > 0 {
		sudoArgs = append(sudoArgs, "--preserve-env="+strings.Join(names, ","))
	}
	sudoArgs = append(sudoArgs, name)
	sudoArgs = append(sudoArgs, args...)
	return sudoArgs, childEnv
}

// validEnvValue rejects values that cannot be carried in an environment
// variable.
func validEnvValue(env map[string]string) error {
	for key, value := range env {
		if strings.ContainsAny(key, "=\x00") || strings.ContainsRune(value, 0) {
			return fmt.Errorf("invalid environment entry %q", key)
		}
	}
	return nil
}

// RunSudoWithEnv runs a sudo command with extra environment variables, so
// secrets stay out of the process arguments.
func (e *Executor) RunSudoWithEnv(ctx context.Context, env map[string]string, name string, args ...string) (*Result, error) {
	if err := validEnvValue(env); err != nil {
		return nil, err
	}
	sudoArgs, childEnv := sudoEnvArgs(env, name, args)
	return e.runWithEnv(ctx, childEnv, nil, "/usr/bin/sudo", sudoArgs...)
}

// RunSudoWithInputStreamEnv streams stdin into a sudo command that receives
// extra environment variables.
func (e *Executor) RunSudoWithInputStreamEnv(ctx context.Context, env map[string]string, stdin io.Reader, stderrW io.Writer, name string, args ...string) (int, error) {
	if err := validEnvValue(env); err != nil {
		return 0, err
	}
	sudoArgs, childEnv := sudoEnvArgs(env, name, args)
	return e.streamWithEnv(ctx, childEnv, nil, stderrW, stdin, "/usr/bin/sudo", sudoArgs...)
}
