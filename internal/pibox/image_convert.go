package pibox

import (
	"fmt"
	"io"
	"os"

	qcow2reader "github.com/lima-vm/go-qcow2reader"
)

func convertImageToRaw(srcPath, dstPath string, size int64) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	img, err := qcow2reader.Open(src)
	if err != nil {
		return fmt.Errorf("apertura immagine sorgente: %w", err)
	}
	defer img.Close()
	if err := img.Readable(); err != nil {
		return fmt.Errorf("immagine sorgente non leggibile: %w", err)
	}
	virtualSize := img.Size()
	if virtualSize <= 0 {
		return fmt.Errorf("dimensione virtuale immagine sconosciuta")
	}
	if size < virtualSize {
		size = virtualSize
	}

	tmp := dstPath + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := out.Truncate(size); err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if _, err := io.Copy(out, io.NewSectionReader(img, 0, virtualSize)); err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("conversione immagine raw: %w", err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dstPath)
}

func parseSizeBytes(s string) (int64, error) {
	var n int64
	var unit string
	if _, err := fmt.Sscanf(s, "%d%s", &n, &unit); err != nil {
		return 0, err
	}
	switch unit {
	case "", "B":
		return n, nil
	case "K", "k":
		return n << 10, nil
	case "M", "m":
		return n << 20, nil
	case "G", "g":
		return n << 30, nil
	case "T", "t":
		return n << 40, nil
	default:
		return 0, fmt.Errorf("unità dimensione non supportata: %s", unit)
	}
}
