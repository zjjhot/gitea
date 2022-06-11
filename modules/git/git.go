// Copyright 2015 The Gogs Authors. All rights reserved.
// Copyright 2017 The Gitea Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"code.gitea.io/gitea/modules/log"
	"code.gitea.io/gitea/modules/setting"

	"github.com/hashicorp/go-version"
)

var (
	// GitVersionRequired is the minimum Git version required
	// At the moment, all code for git 1.x are not changed, if some users want to test with old git client
	// or bypass the check, they still have a chance to edit this variable manually.
	// If everything works fine, the code for git 1.x could be removed in a separate PR before 1.17 frozen.
	GitVersionRequired = "2.0.0"

	// GitExecutable is the command name of git
	// Could be updated to an absolute path while initialization
	GitExecutable = "git"

	// DefaultContext is the default context to run git commands in
	// will be overwritten by InitXxx with HammerContext
	DefaultContext = context.Background()

	// SupportProcReceive version >= 2.29.0
	SupportProcReceive bool

	gitVersion *version.Version
)

// loadGitVersion returns current Git version from shell. Internal usage only.
func loadGitVersion() (*version.Version, error) {
	// doesn't need RWMutex because its exec by Init()
	if gitVersion != nil {
		return gitVersion, nil
	}

	stdout, _, runErr := NewCommand(DefaultContext, "version").RunStdString(nil)
	if runErr != nil {
		return nil, runErr
	}

	fields := strings.Fields(stdout)
	if len(fields) < 3 {
		return nil, fmt.Errorf("invalid git version output: %s", stdout)
	}

	var versionString string

	// Handle special case on Windows.
	i := strings.Index(fields[2], "windows")
	if i >= 1 {
		versionString = fields[2][:i-1]
	} else {
		versionString = fields[2]
	}

	var err error
	gitVersion, err = version.NewVersion(versionString)
	return gitVersion, err
}

// SetExecutablePath changes the path of git executable and checks the file permission and version.
func SetExecutablePath(path string) error {
	// If path is empty, we use the default value of GitExecutable "git" to search for the location of git.
	if path != "" {
		GitExecutable = path
	}
	absPath, err := exec.LookPath(GitExecutable)
	if err != nil {
		return fmt.Errorf("git not found: %w", err)
	}
	GitExecutable = absPath

	_, err = loadGitVersion()
	if err != nil {
		return fmt.Errorf("unable to load git version: %w", err)
	}

	versionRequired, err := version.NewVersion(GitVersionRequired)
	if err != nil {
		return err
	}

	if gitVersion.LessThan(versionRequired) {
		moreHint := "get git: https://git-scm.com/download/"
		if runtime.GOOS == "linux" {
			// there are a lot of CentOS/RHEL users using old git, so we add a special hint for them
			if _, err = os.Stat("/etc/redhat-release"); err == nil {
				// ius.io is the recommended official(git-scm.com) method to install git
				moreHint = "get git: https://git-scm.com/download/linux and https://ius.io"
			}
		}
		return fmt.Errorf("installed git version %q is not supported, Gitea requires git version >= %q, %s", gitVersion.Original(), GitVersionRequired, moreHint)
	}

	return nil
}

// VersionInfo returns git version information
func VersionInfo() string {
	if gitVersion == nil {
		return "(git not found)"
	}
	format := "%s"
	args := []interface{}{gitVersion.Original()}
	// Since git wire protocol has been released from git v2.18
	if setting.Git.EnableAutoGitWireProtocol && CheckGitVersionAtLeast("2.18") == nil {
		format += ", Wire Protocol %s Enabled"
		args = append(args, "Version 2") // for focus color
	}

	return fmt.Sprintf(format, args...)
}

// HomeDir is the home dir for git to store the global config file used by Gitea internally
func HomeDir() string {
	if setting.RepoRootPath == "" {
		// TODO: now, some unit test code call the git module directly without initialization, which is incorrect.
		// at the moment, we just use a temp HomeDir to prevent from conflicting with user's git config
		// in the future, the git module should be initialized first before use.
		tmpHomeDir := filepath.Join(os.TempDir(), "gitea-temp-home")
		log.Error("Git's HomeDir is empty (RepoRootPath is empty), the git module is not initialized correctly, using a temp HomeDir (%s) temporarily", tmpHomeDir)
		return tmpHomeDir
	}
	return setting.RepoRootPath
}

// InitSimple initializes git module with a very simple step, no config changes, no global command arguments.
// This method doesn't change anything to filesystem. At the moment, it is only used by "git serv" sub-command, no data-race
func InitSimple(ctx context.Context) error {
	DefaultContext = ctx

	if setting.Git.Timeout.Default > 0 {
		defaultCommandExecutionTimeout = time.Duration(setting.Git.Timeout.Default) * time.Second
	}

	if err := SetExecutablePath(setting.Git.Path); err != nil {
		return err
	}

	// force cleanup args
	globalCommandArgs = []string{}

	return nil
}

var initOnce sync.Once

// InitOnceWithSync initializes git module with version check and change global variables, sync gitconfig.
// This method will update the global variables ONLY ONCE (just like git.CheckLFSVersion -- which is not ideal too),
// otherwise there will be data-race problem at the moment.
func InitOnceWithSync(ctx context.Context) (err error) {
	initOnce.Do(func() {
		err = InitSimple(ctx)
		if err != nil {
			return
		}

		// Since git wire protocol has been released from git v2.18
		if setting.Git.EnableAutoGitWireProtocol && CheckGitVersionAtLeast("2.18") == nil {
			globalCommandArgs = append(globalCommandArgs, "-c", "protocol.version=2")
		}

		// By default partial clones are disabled, enable them from git v2.22
		if !setting.Git.DisablePartialClone && CheckGitVersionAtLeast("2.22") == nil {
			globalCommandArgs = append(globalCommandArgs, "-c", "uploadpack.allowfilter=true", "-c", "uploadpack.allowAnySHA1InWant=true")
		}

		// Explicitly disable credential helper, otherwise Git credentials might leak
		if CheckGitVersionAtLeast("2.9") == nil {
			globalCommandArgs = append(globalCommandArgs, "-c", "credential.helper=")
		}

		SupportProcReceive = CheckGitVersionAtLeast("2.29") == nil
	})
	if err != nil {
		return err
	}
	return syncGitConfig()
}

// syncGitConfig only modifies gitconfig, won't change global variables (otherwise there will be data-race problem)
func syncGitConfig() (err error) {
	if err = os.MkdirAll(HomeDir(), os.ModePerm); err != nil {
		return fmt.Errorf("unable to create directory %s, err: %w", setting.RepoRootPath, err)
	}

	// Git requires setting user.name and user.email in order to commit changes - old comment: "if they're not set just add some defaults"
	// TODO: need to confirm whether users really need to change these values manually. It seems that these values are dummy only and not really used.
	// If these values are not really used, then they can be set (overwritten) directly without considering about existence.
	for configKey, defaultValue := range map[string]string{
		"user.name":  "Gitea",
		"user.email": "gitea@fake.local",
	} {
		if err := configSetNonExist(configKey, defaultValue); err != nil {
			return err
		}
	}

	// Set git some configurations - these must be set to these values for gitea to work correctly
	if err := configSet("core.quotePath", "false"); err != nil {
		return err
	}

	if CheckGitVersionAtLeast("2.10") == nil {
		if err := configSet("receive.advertisePushOptions", "true"); err != nil {
			return err
		}
	}

	if CheckGitVersionAtLeast("2.18") == nil {
		if err := configSet("core.commitGraph", "true"); err != nil {
			return err
		}
		if err := configSet("gc.writeCommitGraph", "true"); err != nil {
			return err
		}
	}

	if SupportProcReceive {
		// set support for AGit flow
		if err := configAddNonExist("receive.procReceiveRefs", "refs/for"); err != nil {
			return err
		}
	} else {
		if err := configUnsetAll("receive.procReceiveRefs", "refs/for"); err != nil {
			return err
		}
	}

	if runtime.GOOS == "windows" {
		if err := configSet("core.longpaths", "true"); err != nil {
			return err
		}
		if setting.Git.DisableCoreProtectNTFS {
			err = configSet("core.protectNTFS", "false")
		} else {
			err = configUnsetAll("core.protectNTFS", "false")
		}
		if err != nil {
			return err
		}
	}

	return nil
}

// CheckGitVersionAtLeast check git version is at least the constraint version
func CheckGitVersionAtLeast(atLeast string) error {
	if _, err := loadGitVersion(); err != nil {
		return err
	}
	atLeastVersion, err := version.NewVersion(atLeast)
	if err != nil {
		return err
	}
	if gitVersion.Compare(atLeastVersion) < 0 {
		return fmt.Errorf("installed git binary version %s is not at least %s", gitVersion.Original(), atLeast)
	}
	return nil
}

func configSet(key, value string) error {
	stdout, _, err := NewCommand(DefaultContext, "config", "--get", key).RunStdString(nil)
	if err != nil && !err.IsExitCode(1) {
		return fmt.Errorf("failed to get git config %s, err: %w", key, err)
	}

	currValue := strings.TrimSpace(stdout)
	if currValue == value {
		return nil
	}

	_, _, err = NewCommand(DefaultContext, "config", "--global", key, value).RunStdString(nil)
	if err != nil {
		return fmt.Errorf("failed to set git global config %s, err: %w", key, err)
	}

	return nil
}

func configSetNonExist(key, value string) error {
	_, _, err := NewCommand(DefaultContext, "config", "--get", key).RunStdString(nil)
	if err == nil {
		// already exist
		return nil
	}
	if err.IsExitCode(1) {
		// not exist, set new config
		_, _, err = NewCommand(DefaultContext, "config", "--global", key, value).RunStdString(nil)
		if err != nil {
			return fmt.Errorf("failed to set git global config %s, err: %w", key, err)
		}
		return nil
	}

	return fmt.Errorf("failed to get git config %s, err: %w", key, err)
}

func configAddNonExist(key, value string) error {
	_, _, err := NewCommand(DefaultContext, "config", "--fixed-value", "--get", key, value).RunStdString(nil)
	if err == nil {
		// already exist
		return nil
	}
	if err.IsExitCode(1) {
		// not exist, add new config
		_, _, err = NewCommand(DefaultContext, "config", "--global", "--add", key, value).RunStdString(nil)
		if err != nil {
			return fmt.Errorf("failed to add git global config %s, err: %w", key, err)
		}
		return nil
	}
	return fmt.Errorf("failed to get git config %s, err: %w", key, err)
}

func configUnsetAll(key, value string) error {
	_, _, err := NewCommand(DefaultContext, "config", "--get", key).RunStdString(nil)
	if err == nil {
		// exist, need to remove
		_, _, err = NewCommand(DefaultContext, "config", "--global", "--fixed-value", "--unset-all", key, value).RunStdString(nil)
		if err != nil {
			return fmt.Errorf("failed to unset git global config %s, err: %w", key, err)
		}
		return nil
	}
	if err.IsExitCode(1) {
		// not exist
		return nil
	}
	return fmt.Errorf("failed to get git config %s, err: %w", key, err)
}

// Fsck verifies the connectivity and validity of the objects in the database
func Fsck(ctx context.Context, repoPath string, timeout time.Duration, args ...string) error {
	return NewCommand(ctx, "fsck").AddArguments(args...).Run(&RunOpts{Timeout: timeout, Dir: repoPath})
}
