package pix

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	appName      = "pix"
	vmRoot       = "/var/lib/pix"
	resultBranch = "pi-result"
)

type RepoConfig struct {
	SchemaVersion int    `json:"schema_version"`
	RepoID        string `json:"repo_id"`
	VMRepoPath    string `json:"vm_repo_path"`
	WorktreePath  string `json:"worktree_path"`
	BridgePath    string `json:"bridge_path"`
	DefaultBranch string `json:"default_branch"`
}

func NewRepoConfig(repoID string) RepoConfig {
	vmRepoPath := fmt.Sprintf("%s/repos/%s", vmRoot, repoID)
	return RepoConfig{
		SchemaVersion: 1,
		RepoID:        repoID,
		VMRepoPath:    vmRepoPath,
		WorktreePath:  vmRepoPath + "/worktree",
		BridgePath:    vmRepoPath + "/bridge.git",
		DefaultBranch: resultBranch,
	}
}

func stateHome() (string, error) {
	if override := os.Getenv("PIX_HOME"); override != "" {
		return filepath.Abs(override)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("lettura home utente: %w", err)
	}
	return filepath.Join(home, ".pix"), nil
}

func ensureStateTree() (string, error) {
	root, err := stateHome()
	if err != nil {
		return "", err
	}
	for _, path := range []string{
		filepath.Join(root, "images"),
		filepath.Join(root, "vm", "default", "ssh"),
		filepath.Join(root, "logs"),
	} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return "", fmt.Errorf("creazione %s: %w", path, err)
		}
	}
	return root, nil
}

func repoConfigPath(gitDir string) string {
	return filepath.Join(gitDir, appName, "config.json")
}

func readRepoConfig(gitDir string) (RepoConfig, error) {
	path := repoConfigPath(gitDir)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return RepoConfig{}, userError("Questo repo non è registrato con pix.\n\nEsegui:\n  pix init repo")
	}
	if err != nil {
		return RepoConfig{}, fmt.Errorf("lettura %s: %w", path, err)
	}
	var cfg RepoConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return RepoConfig{}, fmt.Errorf("config repo pix non valida %s: %w", path, err)
	}
	if cfg.SchemaVersion != 1 || cfg.RepoID == "" || cfg.WorktreePath == "" || cfg.BridgePath == "" {
		return RepoConfig{}, userError("Config repo pix incompleta o non supportata.")
	}
	return cfg, nil
}

func writeRepoConfig(gitDir string, cfg RepoConfig) error {
	path := repoConfigPath(gitDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creazione %s: %w", filepath.Dir(path), err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("scrittura %s: %w", path, err)
	}
	return nil
}
