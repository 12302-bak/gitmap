// Copyright © 2016-present Bjørn Erik Pedersen <bjorn.erik.pedersen@gmail.com>.
//
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package gitmap

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// GitMap maps filenames to Git revision information.
type GitMap map[string]*GitInfo

// GitInfo holds information about a Git commit.
type GitInfo struct {
	Hash            string    // Commit hash
	AbbreviatedHash string    // Abbreviated commit hash
	Subject         string    // The commit message's subject/title line
	AuthorName      string    // The author name, respecting .mailmap
	AuthorEmail     string    // The author email address, respecting .mailmap
	AuthorDate      time.Time // The author date
}

// Map creates a GitMap from the given repository path and revision.
// Use HEAD as revision for the latest.
func Map(repository, revision string) (GitMap, error) {
	m := make(GitMap)

	gitLogArgs := fmt.Sprintf(
		"-C %s log --name-only --format=format:%%x1e%%H%%x1f%%h%%x1f%%s%%x1f%%aN%%x1f%%aE%%x1f%%ai %s",
		repository,
		revision,
	)

	out, err := exec.Command("git", strings.Fields(gitLogArgs)...).Output()

	if err != nil {
		return nil, err
	}

	entriesStr := string(out)
	entriesStr = strings.Trim(entriesStr, "\n\x1e'")
	entries := strings.Split(entriesStr, "\x1e")

	for _, e := range entries {
		lines := strings.Split(e, "\n")
		gitInfo, err := toGitInfo(lines[0])

		if err != nil {
			return nil, err
		}

		for _, filename := range lines[1:] {
			filename := strings.TrimSpace(filename)
			if filename == "" {
				continue
			}
			m[filename] = gitInfo
		}
	}

	return m, nil
}

func toGitInfo(entry string) (*GitInfo, error) {
	items := strings.Split(entry, "\x1f")
	authorDate, err := time.Parse("2006-01-02 15:04:05 -0700", items[5])

	if err != nil {
		return nil, err
	}

	return &GitInfo{
		Hash:            items[0],
		AbbreviatedHash: items[1],
		Subject:         items[2],
		AuthorName:      items[3],
		AuthorEmail:     items[4],
		AuthorDate:      authorDate,
	}, nil
}