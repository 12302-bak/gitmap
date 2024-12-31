// Copyright 2024 Bjørn Erik Pedersen <bjorn.erik.pedersen@gmail.com>.
//
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package gitmap

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var (
	// will be modified during tests
	gitExec string

	ErrGitNotFound = errors.New("git executable not found in $PATH")
)

type GitRepo struct {
	// TopLevelAbsPath contains the absolute path of the top-level directory.
	// This is similar to the answer from "git rev-parse --show-toplevel"
	// except symbolic link is not followed on non-Windows platforms.
	// Note that this follows Git's way of handling paths, so expect to get forward slashes,
	// even on Windows.
	TopLevelAbsPath string

	// The files in this Git repository.
	Files GitMap
}

// GitMap maps filenames to Git revision information.
type GitMap map[string]*GitInfo

type ContentGitInfo = map[string]GitInfo

// GitInfo holds information about a Git commit.
type GitInfo struct {
	Hash            string    `json:"hash"`            // Commit hash
	AbbreviatedHash string    `json:"abbreviatedHash"` // Abbreviated commit hash
	Subject         string    `json:"subject"`         // The commit message's subject/title line
	AuthorName      string    `json:"authorName"`      // The author name, respecting .mailmap
	AuthorEmail     string    `json:"authorEmail"`     // The author email address, respecting .mailmap
	AuthorDate      time.Time `json:"authorDate"`      // The author date
	CommitDate      time.Time `json:"commitDate"`      // The commit date
	CreateDate      time.Time `json:"createDate"`      // The create date
	FromGetJson     *GitInfo
	MergeCreateDate time.Time `json:"mergeCreateDate"` // The merge create date
	MergeUpdateDate time.Time `json:"mergeUpdateDate"` // The merge update date
	Year            string    `json:"year"`            // timeline year for group
	Body            string    `json:"body"`            // The commit message body
}

// Runner is an interface for running Git commands,
// as implemented buy *exec.Cmd.
type Runner interface {
	Run() error
}

// Options for the Map function
type Options struct {
	Repository        string // Path to the repository to map
	Revision          string // Use blank or HEAD for the currently active revision
	GetGitCommandFunc func(stdout, stderr io.Writer, args ...string) (Runner, error)
}

// Map creates a GitRepo with a file map from the given options.
func Map(opts Options) (*GitRepo, error) {
	if opts.GetGitCommandFunc == nil {
		opts.GetGitCommandFunc = func(stdout, stderr io.Writer, args ...string) (Runner, error) {
			cmd := exec.Command(gitExec, args...)
			cmd.Stdout = stdout
			cmd.Stderr = stderr
			return cmd, nil
		}
	}

	m := make(GitMap)

	parentDir := filepath.Dir(opts.Repository)
	targetPath := filepath.Join(parentDir, "assets", "git-info", "contentGitInfo.json")
	gim, err := ReadJSONFile(targetPath)
	if err != nil {
		fmt.Printf("targetPath: %s %s\n", targetPath, err)
	}

	// First get the top level repo path
	absRepoPath, err := filepath.Abs(opts.Repository)
	if err != nil {
		return nil, err
	}

	out, err := git(opts, "-C", opts.Repository, "rev-parse", "--show-cdup")
	if err != nil {
		return nil, err
	}

	cdUp := strings.TrimSpace(string(out))
	topLevelPath := filepath.ToSlash(filepath.Join(absRepoPath, cdUp))

	gitLogArgs := strings.Fields(fmt.Sprintf(
		`--name-only --no-merges --format=format:%%x1e%%H%%x1f%%h%%x1f%%s%%x1f%%aN%%x1f%%aE%%x1f%%ai%%x1f%%ci%%x1f%%b%%x1d %s`,
		opts.Revision,
	))

	gitLogArgs = append([]string{"-c", "diff.renames=0", "-c", "log.showSignature=0", "-C", opts.Repository, "log"}, gitLogArgs...)
	out, err = git(opts, gitLogArgs...)
	if err != nil {
		return nil, err
	}

	entriesStr := strings.Trim(out, "\n\x1e'")
	entries := strings.Split(entriesStr, "\x1e")

	for _, e := range entries {
		lines := strings.Split(e, "\x1d")
		filenames := strings.Split(lines[1], "\n")

		for _, filename := range filenames {

			gitInfo, err := toGitInfo(lines[0])
			if err != nil {
				return nil, err
			}
			filename := strings.TrimSpace(filename)
			if filename == "" {
				continue
			}
			if originGi, ok := m[filename]; !ok {
				m[filename] = gitInfo
			} else {
				originGi.CreateDate = gitInfo.AuthorDate
				originGi.MergeCreateDate = gitInfo.AuthorDate
			}

			calcInfo := m[filename]
			if jsonInfo, exists := gim[filename]; exists {
				calcInfo.FromGetJson = &jsonInfo

				if jsonInfo.CreateDate.Before(calcInfo.CreateDate) {
					calcInfo.MergeCreateDate = jsonInfo.CreateDate
				}
				if jsonInfo.AuthorDate.After(calcInfo.AuthorDate) {
					calcInfo.MergeUpdateDate = jsonInfo.AuthorDate
				}
			}
			calcInfo.Year = calcInfo.MergeCreateDate.Format("2006")
		}
	}

	return &GitRepo{Files: m, TopLevelAbsPath: topLevelPath}, nil
}

// FileExists checks if a file exists.
func FileExists(filename string) bool {
	_, err := os.Stat(filename)
	return !os.IsNotExist(err)
}
func ReadJSONFile(filename string) (ContentGitInfo, error) {
	if !FileExists(filename) {
		return nil, errors.New("file does not exist")
	}

	// Read the file content
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	// Parse the JSON data
	var result ContentGitInfo
	err = json.Unmarshal(data, &result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func git(opts Options, args ...string) (string, error) {
	var outBuff bytes.Buffer
	var errBuff bytes.Buffer
	cmd, err := opts.GetGitCommandFunc(&outBuff, &errBuff, args...)
	if err != nil {
		return "", err
	}
	err = cmd.Run()
	if err != nil {
		if ee, ok := err.(*exec.Error); ok {
			if ee.Err == exec.ErrNotFound {
				return "", ErrGitNotFound
			}
		}
		return "", errors.New(strings.TrimSpace(errBuff.String()))
	}
	return outBuff.String(), nil
}

func toGitInfo(entry string) (*GitInfo, error) {
	items := strings.Split(entry, "\x1f")
	if len(items) == 7 {
		items = append(items, "")
	}
	authorDate, err := time.Parse("2006-01-02 15:04:05 -0700", items[5])
	if err != nil {
		return nil, err
	}
	commitDate, err := time.Parse("2006-01-02 15:04:05 -0700", items[6])
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
		CommitDate:      commitDate,
		CreateDate:      authorDate,
		FromGetJson:     nil,
		MergeCreateDate: authorDate,
		MergeUpdateDate: authorDate,
		Year:            "1970",
		Body:            strings.TrimSpace(items[7]),
	}, nil
}

func init() {
	initDefaults()
}

func initDefaults() {
	gitExec = "git"
}
