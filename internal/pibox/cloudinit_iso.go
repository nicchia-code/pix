package pibox

import (
	"os"

	"github.com/kdomanski/iso9660"
)

func createNoCloudISO(seedPath, userDataPath, metaDataPath string) error {
	writer, err := iso9660.NewWriter()
	if err != nil {
		return err
	}
	defer writer.Cleanup()

	userData, err := os.Open(userDataPath)
	if err != nil {
		return err
	}
	defer userData.Close()
	if err := writer.AddFile(userData, "user-data"); err != nil {
		return err
	}

	metaData, err := os.Open(metaDataPath)
	if err != nil {
		return err
	}
	defer metaData.Close()
	if err := writer.AddFile(metaData, "meta-data"); err != nil {
		return err
	}

	out, err := os.OpenFile(seedPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := writer.WriteTo(out, "cidata"); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
