package cliapp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"wor/internal/osutil"
	"wor/internal/version"
)

// downloadBase is where the published releases live. Overridable with
// WOR_DOWNLOAD_BASE, which exists so this can be pointed at a local
// `php -S` while working on the download site -- there is otherwise no
// way to exercise this command without publishing a real release.
const downloadBase = "https://wor.worapong.com/download"

// upgradeHTTPTimeout bounds every request this command makes. A release
// archive is around 30 MB, so this is generous enough for a slow link
// and still short enough that an unreachable host fails rather than
// hanging a terminal indefinitely.
const upgradeHTTPTimeout = 10 * time.Minute

// release is what download/version.php reports.
type release struct {
	Tag     string `json:"tag"`
	Version string `json:"version"`
	Build   int    `json:"build"`
	Tarball string `json:"tarball"`
	SHA256  string `json:"sha256"`
}

// cmdUpgrade compares the running binary against the release the
// download site currently publishes, shows both, and -- once the
// operator agrees -- installs the newer one.
//
// It asks download/version.php rather than looking at the releases
// directory, for the same reason installer.sh does: the archive it then
// downloads is a versioned, immutable URL, which is the whole point of
// that endpoint existing (see public/download/version.php).
//
// Installation is handed to the install.sh inside the downloaded
// archive rather than reimplemented here. That script is the tested
// install path, it already knows where the binary goes and what else a
// release needs, and a second implementation of "how to install wor"
// living in wor itself is exactly the kind of thing that drifts.
// Replacing the running binary is safe: install(1) unlinks the target
// before writing, so this process keeps running from the old inode.
func (a *App) cmdUpgrade(args []string) error {
	if osutil.IsWindows() {
		return a.errf("wor upgrade installs through install.sh, which is a unix shell script.\n" +
			"On Windows, download the new release from " + downloadBase + " and replace the binary.")
	}
	fl := parseFlags(args)
	assumeYes := fl.Has("yes") || fl.Has("y")

	latest, err := fetchLatestRelease()
	if err != nil {
		return a.errf("could not ask %s which release is current: %s", versionEndpoint(), err)
	}

	installedBuild, buildKnown := version.BuildNumber()
	a.printUpgradeComparison(latest, installedBuild, buildKnown)

	switch {
	case !buildKnown:
		// Refusing would be unhelpful -- this is the state a binary
		// built by hand is in, and upgrading to a published release is
		// a perfectly reasonable thing to want from it. Just do not
		// pretend to have compared anything.
		a.warn("This binary has no build number, so there is nothing to compare against.")
	case latest.Version == Version && latest.Build == installedBuild:
		a.ok("Already on the current release (%s).", latest.Tag)
		return nil
	case latest.Version == Version && latest.Build < installedBuild:
		a.warn("This binary (build %d) is newer than the published release (build %d).",
			installedBuild, latest.Build)
		a.info("That normally means a local build. Continuing would install the older, published one.")
	}

	if !assumeYes && !a.confirmYesDefaultYes(fmt.Sprintf("Install %s?", latest.Tag)) {
		a.info("Nothing was changed.")
		return nil
	}
	return a.installRelease(latest)
}

// printUpgradeComparison shows what is installed against what is
// published, before anything is downloaded. Showing it first, and
// always, is the point of the command: an operator should be able to run
// `wor upgrade`, read two lines and press n.
func (a *App) printUpgradeComparison(latest release, installedBuild int, buildKnown bool) {
	installed := fmt.Sprintf("v%s-b%d", Version, installedBuild)
	if !buildKnown {
		installed = fmt.Sprintf("v%s (build unknown)", Version)
	}
	fmt.Fprintln(a.Out)
	fmt.Fprintln(a.Out, "Upgrade")
	fmt.Fprintln(a.Out, "-------")
	fmt.Fprintf(a.Out, "Installed : %s\n", installed)
	fmt.Fprintf(a.Out, "Published : %s\n", latest.Tag)
	fmt.Fprintf(a.Out, "Source    : %s/%s\n", baseURL(), latest.Tarball)
	fmt.Fprintln(a.Out)
}

func versionEndpoint() string { return baseURL() + "/version.php?format=json" }

// baseURL resolves the download site, honouring WOR_DOWNLOAD_BASE.
func baseURL() string {
	if v := strings.TrimSpace(os.Getenv("WOR_DOWNLOAD_BASE")); v != "" {
		return strings.TrimSuffix(v, "/")
	}
	return downloadBase
}

// fetchLatestRelease asks version.php what to install.
func fetchLatestRelease() (release, error) {
	client := &http.Client{Timeout: upgradeHTTPTimeout}
	resp, err := client.Get(versionEndpoint())
	if err != nil {
		return release{}, err
	}
	defer resp.Body.Close()

	// version.php answers 503 while no complete release is published --
	// mid-publish, or after one was withdrawn. Report that as itself
	// rather than as a parse failure.
	if resp.StatusCode == http.StatusServiceUnavailable {
		return release{}, fmt.Errorf("the download site has no published release right now (HTTP 503)")
	}
	if resp.StatusCode != http.StatusOK {
		return release{}, fmt.Errorf("unexpected HTTP %d", resp.StatusCode)
	}

	var rel release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&rel); err != nil {
		return release{}, fmt.Errorf("could not read the reply: %w", err)
	}
	if rel.Tag == "" || rel.Tarball == "" {
		return release{}, fmt.Errorf("the reply named no release")
	}
	return rel, nil
}

// installRelease downloads the release, checks it against its published
// checksum, and runs the install.sh inside it.
func (a *App) installRelease(rel release) error {
	dir, err := os.MkdirTemp("", "wor-upgrade-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	archive := filepath.Join(dir, "package.tar.gz")
	a.info("Downloading %s", rel.Tag)
	if err := downloadTo(baseURL()+"/"+rel.Tarball, archive); err != nil {
		return a.errf("could not download %s: %s", rel.Tag, err)
	}

	if err := a.verifyChecksum(rel, archive); err != nil {
		return err
	}

	a.info("Extracting")
	if out, err := exec.Command("tar", "-xzf", archive, "-C", dir).CombinedOutput(); err != nil {
		return a.errf("could not extract %s: %s: %s", rel.Tag, err, strings.TrimSpace(string(out)))
	}

	script, err := findInstallScript(dir)
	if err != nil {
		return a.errf("%s", err)
	}
	if err := os.Chmod(script, 0o755); err != nil {
		return err
	}

	a.info("Running the installer from %s", rel.Tag)
	cmd, err := osutil.SudoCommand(script)
	if err != nil {
		return err
	}
	// Wired straight to the terminal, not captured: install.sh asks its
	// own questions (which optional packages to add, whether to touch a
	// shell rc file) and capturing its output would hang on the first
	// prompt with nothing on screen to explain why.
	cmd.Dir = filepath.Dir(script)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, a.Out, a.Err
	if err := cmd.Run(); err != nil {
		return a.errf("the installer from %s did not finish: %s", rel.Tag, err)
	}
	a.ok("Upgraded to %s. Run `wor version` to confirm.", rel.Tag)
	return nil
}

// verifyChecksum checks the downloaded archive against the published
// .sha256, which release.sh writes beside every release.
//
// A missing checksum file is a warning rather than a failure, matching
// installer.sh: releases published before checksums existed are still
// installable. A checksum that is present and does not match is always
// fatal.
func (a *App) verifyChecksum(rel release, archive string) error {
	if rel.SHA256 == "" {
		a.warn("This release publishes no checksum; skipping verification.")
		return nil
	}
	sums := filepath.Join(filepath.Dir(archive), "published.sha256")
	if err := downloadTo(baseURL()+"/"+rel.SHA256, sums); err != nil {
		a.warn("Could not fetch the published checksum (%s); skipping verification.", err)
		return nil
	}

	want, err := checksumFor(sums, rel.Tag+".tar.gz")
	if err != nil {
		a.warn("%s; skipping verification.", err)
		return nil
	}
	got, err := fileSHA256(archive)
	if err != nil {
		return err
	}
	if !strings.EqualFold(got, want) {
		return a.errf("checksum mismatch for %s.\nThe download is corrupt or has been tampered with. Nothing was installed.", rel.Tag)
	}
	a.ok("Checksum verified")
	return nil
}

// checksumFor pulls one file's hash out of a `sha256sum` style file,
// whose lines are "<hex>  <filename>". The published file also lists the
// .zip, which is not what was downloaded.
func checksumFor(path, wantName string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("could not read the published checksum: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == wantName {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("the published checksum does not cover %s", wantName)
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func downloadTo(url, dest string) error {
	client := &http.Client{Timeout: upgradeHTTPTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return err
	}
	return f.Sync()
}

// findInstallScript locates install.sh in the extracted archive. The
// archive puts it at wor-host/install.sh, but the directory name inside
// is not something this command should depend on -- release.sh is free
// to change it -- so it is searched for one level down as installer.sh
// does.
func findInstallScript(root string) (string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		candidate := filepath.Join(root, e.Name(), "install.sh")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no install.sh inside the downloaded release")
}
