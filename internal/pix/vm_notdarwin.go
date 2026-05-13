//go:build !darwin

package pix

import "context"

func (v VM) ensureAppleVirtualization(ctx context.Context, root, statePath string) error {
	return nil
}

func (a *App) runDarwinVMHelper(ctx context.Context, args []string) error {
	return userError("Helper macOS non disponibile su questa piattaforma.")
}
