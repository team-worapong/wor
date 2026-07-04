package ssl

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"wor/internal/osutil"
)

// InstallCustom copies a user-supplied cert/key pair into wor's SSL
// state directory, matching lib/providers/ssl/custom.sh
// ssl_install_custom(). Returns the destination cert/key paths.
func InstallCustom(sslRoot, host, certPath, keyPath string) (dstCert, dstKey string, err error) {
	if _, err := os.Stat(certPath); err != nil {
		return "", "", fmt.Errorf("certificate file not found: %s", certPath)
	}
	if _, err := os.Stat(keyPath); err != nil {
		return "", "", fmt.Errorf("private key file not found: %s", keyPath)
	}
	dir := HostDir(sslRoot, host)
	if err := osutil.EnsureDir(dir); err != nil {
		return "", "", err
	}
	dstCert = filepath.Join(dir, "fullchain.pem")
	dstKey = filepath.Join(dir, "privkey.pem")
	if err := copyFile(certPath, dstCert); err != nil {
		return "", "", err
	}
	if err := copyFile(keyPath, dstKey); err != nil {
		return "", "", err
	}
	_ = os.Chmod(dstKey, 0o600)
	return dstCert, dstKey, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
