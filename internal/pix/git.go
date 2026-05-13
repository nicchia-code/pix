package pix

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
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

func gitStatus(ctx context.Context, r commandRunner, root string) (string, error) {
	return commandText(ctx, r, root, "git", "status", "--porcelain")
}

func requireCleanWorktree(ctx context.Context, r commandRunner, root string) error {
	status, err := gitStatus(ctx, r, root)
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

func gitHasHead(ctx context.Context, r commandRunner, root string) (bool, error) {
	_, _, err := r.Run(ctx, root, nil, "git", "rev-parse", "--verify", "HEAD")
	if err == nil {
		return true, nil
	}
	return false, nil
}

func gitWorktreeTar(ctx context.Context, r commandRunner, root string) ([]byte, error) {
	stdout, _, err := r.Run(ctx, root, nil, "git", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	paths := strings.Split(string(stdout), "\x00")
	for _, rel := range paths {
		if rel == "" {
			continue
		}
		if err := addPathToTar(tw, root, rel); err != nil {
			_ = tw.Close()
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func addPathToTar(tw *tar.Writer, root, rel string) error {
	cleanRel := filepath.ToSlash(filepath.Clean(rel))
	if cleanRel == "." || strings.HasPrefix(cleanRel, "../") || filepath.IsAbs(cleanRel) {
		return nil
	}
	fullPath := filepath.Join(root, filepath.FromSlash(cleanRel))
	info, err := os.Lstat(fullPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var link string
	if info.Mode()&os.ModeSymlink != 0 {
		link, err = os.Readlink(fullPath)
		if err != nil {
			return err
		}
	}
	hdr, err := tar.FileInfoHeader(info, link)
	if err != nil {
		return err
	}
	hdr.Name = cleanRel
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	f, err := os.Open(fullPath)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(tw, f)
	return err
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
