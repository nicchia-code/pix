//go:build !linux

package pix

import "context"

func (v VM) ensureWSL2Appliance(ctx context.Context, root, statePath string) error {
	return userError("Backend WSL2 non disponibile su questa piattaforma.")
}

func unregisterWSLDistro(ctx context.Context, r commandRunner, distro string) error {
	return nil
}
