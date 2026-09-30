package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// publishGitHub uploads the artifacts of a build to the GitHub release of
// this version, a draft created if needed, with the gh CLI. Update
// manifests go last, so that they never point at a file that is not there.
// Installed apps see the update once the release is published.
func publishGitHub(c *Config, artifacts []string) error {
	if c.Updates == nil || c.Updates.GitHub == "" {
		return fmt.Errorf("-upload needs updates.github in %s", c.configName())
	}
	gh, err := exec.LookPath("gh")
	if err != nil {
		return errors.New("-upload needs the GitHub CLI (https://cli.github.com), signed in with gh auth login or GH_TOKEN")
	}
	repo, tag := c.Updates.GitHub, c.Updates.TagPrefix+c.Version
	var files, manifests []string
	for _, a := range artifacts {
		info, err := os.Stat(a)
		if err != nil || info.IsDir() {
			continue // the macOS bundle ships in the disk image and the update
		}
		name := filepath.Base(a)
		switch {
		case strings.HasPrefix(name, "update-") && strings.HasSuffix(name, ".json"):
			manifests = append(manifests, a)
		case name == installScriptName:
			// The same script for every Linux target.
			if !slices.ContainsFunc(files, func(f string) bool { return filepath.Base(f) == name }) {
				files = append(files, a)
			}
		case strings.HasSuffix(name, ".dmg"), strings.HasSuffix(name, ".exe") && strings.Contains(name, " Setup "),
			strings.HasSuffix(name, ".deb"), strings.HasSuffix(name, ".tar.gz"), strings.HasSuffix(name, ".delta"):
			files = append(files, a)
		}
	}
	if len(files)+len(manifests) == 0 {
		return errors.New("nothing to upload")
	}
	if exec.Command(gh, "release", "view", tag, "--repo", repo).Run() != nil {
		notes, _ := c.releaseNotes()
		logf("creating the draft release %s of %s", tag, repo)
		args := []string{"release", "create", tag, "--repo", repo, "--draft", "--title", tag, "--notes", notes}
		if out, err := exec.Command(gh, args...).CombinedOutput(); err != nil {
			return fmt.Errorf("gh release create: %v\n%s", err, out)
		}
	}
	for _, batch := range [][]string{files, manifests} {
		if len(batch) == 0 {
			continue
		}
		for _, f := range batch {
			logf("uploading %s", filepath.Base(f))
		}
		args := append([]string{"release", "upload", tag, "--repo", repo, "--clobber"}, batch...)
		if out, err := exec.Command(gh, args...).CombinedOutput(); err != nil {
			return fmt.Errorf("gh release upload: %v\n%s", err, out)
		}
	}
	logf("uploaded to the release %s of %s; publish it when every platform is there:\n  gh release edit %s --repo %s --draft=false", tag, repo, tag, repo)
	return nil
}
