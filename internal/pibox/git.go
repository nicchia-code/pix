package pibox

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

var slugPattern = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func gitRoot(ctx context.Context, r commandRunner, dir string) (string, error) {
	root, err := commandText(ctx, r, dir, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return "", userError("Questo comando deve essere eseguito dentro un repo Git.\nSe necessario, inizializza prima il repo con `git init`.")
	}
	return root, nil
}

func gitDir(ctx context.Context, r commandRunner, root string) (string, error) {
	value, err := commandText(ctx, r, root, "git", "rev-parse", "--git-dir")
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value), nil
	}
	return filepath.Clean(filepath.Join(root, value)), nil
}

func currentBranch(ctx context.Context, r commandRunner, root string) (string, error) {
	branch, err := commandText(ctx, r, root, "git", "branch", "--show-current")
	if err != nil {
		return "", err
	}
	if branch == "" {
		return "main", nil
	}
	return branch, nil
}

func requireCleanWorktree(ctx context.Context, r commandRunner, root string) error {
	status, err := commandText(ctx, r, root, "git", "status", "--porcelain")
	if err != nil {
		return err
	}
	if status != "" {
		return userError("Il repo host ha modifiche non committate.\n\nPer sincronizzare nella VM, committa prima le modifiche oppure usa una futura modalità esplicita per includere working tree non committato.\n\nOperazione annullata.")
	}
	return nil
}

func gitArchive(ctx context.Context, r commandRunner, root string) ([]byte, error) {
	stdout, _, err := r.Run(ctx, root, nil, "git", "archive", "--format=tar", "HEAD")
	if err != nil {
		return nil, err
	}
	return stdout, nil
}

func makeRepoID(root string) (string, error) {
	base := strings.ToLower(slugPattern.ReplaceAllString(filepath.Base(root), "-"))
	base = strings.Trim(base, "-._")
	if base == "" {
		base = "repo"
	}
	pathHash := sha256.Sum256([]byte(root))
	random := make([]byte, 3)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generazione repo_id: %w", err)
	}
	return fmt.Sprintf("%s-%s-%s", base, hex.EncodeToString(pathHash[:])[:6], hex.EncodeToString(random)), nil
}
