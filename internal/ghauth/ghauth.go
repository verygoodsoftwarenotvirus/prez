// Package ghauth retrieves a GitHub token from the local gh CLI, so prez
// never has to manage its own credentials.
package ghauth

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Token runs `gh auth token` and returns the trimmed output. It fails with a
// clear message if gh isn't installed or isn't authenticated, rather than
// surfacing a raw exec error.
func Token(ctx context.Context) (string, error) {
	path, err := exec.LookPath("gh")
	if err != nil {
		return "", fmt.Errorf("gh CLI not found on PATH: install it from https://cli.github.com and run `gh auth login`")
	}

	cmd := exec.CommandContext(ctx, path, "auth", "token")
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			return "", fmt.Errorf("gh auth token failed (is `gh auth login` done?): %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("running gh auth token: %w", err)
	}

	token := strings.TrimSpace(string(out))
	if token == "" {
		return "", fmt.Errorf("gh auth token returned an empty token")
	}
	return token, nil
}
