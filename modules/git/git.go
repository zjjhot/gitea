// Copyright 2015 The Gogs Authors. All rights reserved.
// Copyright 2017 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"code.gitea.io/gitea/modules/log"
	"code.gitea.io/gitea/modules/setting"

	"github.com/hashicorp/go-version"
)

// RequiredVersion is the minimum Git version required
const RequiredVersion = "2.0.0"

var (
	// GitExecutable is the command name of git
	// Could be updated to an absolute path while initialization
	GitExecutable = "git"

	// DefaultContext is the default context to run git commands in, must be initialized by git.InitXxx
	DefaultContext context.Context

	DefaultFeatures struct {
		GitVersion *version.Version

		SupportProcReceive bool // >= 2.29
		SupportHashSha256  bool // >= 2.42, SHA-256 repositories no longer an ‘experimental curiosity’
	}
)

// loadGitVersion tries to get the current git version and stores it into a global variable
func loadGitVersion() error {
	// doesn't need RWMutex because it's executed by Init()
	if DefaultFeatures.GitVersion != nil {
		return nil
	}

	stdout, _, runErr := NewCommand(DefaultContext, "version").RunStdString(nil)
	if runErr != nil {
		return runErr
	}

	ver, err := parseGitVersionLine(strings.TrimSpace(stdout))
	if err == nil {
		DefaultFeatures.GitVersion = ver
	}
	return err
}

func parseGitVersionLine(s string) (*version.Version, error) {
	fields := strings.Fields(s)
	if len(fields) < 3 {
		return nil, fmt.Errorf("invalid git version: %q", s)
	}

	// version string is like: "git version 2.29.3" or "git version 2.29.3.windows.1"
	versionString := fields[2]
	if pos := strings.Index(versionString, "windows"); pos >= 1 {
		versionString = versionString[:pos-1]
	}
	return version.NewVersion(versionString)
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

	if err = loadGitVersion(); err != nil {
		return fmt.Errorf("unable to load git version: %w", err)
	}

	versionRequired, err := version.NewVersion(RequiredVersion)
	if err != nil {
		return err
	}

	if DefaultFeatures.GitVersion.LessThan(versionRequired) {
		moreHint := "get git: https://git-scm.com/download/"
		if runtime.GOOS == "linux" {
			// there are a lot of CentOS/RHEL users using old git, so we add a special hint for them
			if _, err = os.Stat("/etc/redhat-release"); err == nil {
				// ius.io is the recommended official(git-scm.com) method to install git
				moreHint = "get git: https://git-scm.com/download/linux and https://ius.io"
			}
		}
		return fmt.Errorf("installed git version %q is not supported, Gitea requires git version >= %q, %s", DefaultFeatures.GitVersion.Original(), RequiredVersion, moreHint)
	}

	if err = checkGitVersionCompatibility(DefaultFeatures.GitVersion); err != nil {
		return fmt.Errorf("installed git version %s has a known compatibility issue with Gitea: %w, please upgrade (or downgrade) git", DefaultFeatures.GitVersion.String(), err)
	}
	return nil
}

// VersionInfo returns git version information
func VersionInfo() string {
	if DefaultFeatures.GitVersion == nil {
		return "(git not found)"
	}
	format := "%s"
	args := []any{DefaultFeatures.GitVersion.Original()}
	// Since git wire protocol has been released from git v2.18
	if setting.Git.EnableAutoGitWireProtocol && CheckGitVersionAtLeast("2.18") == nil {
		format += ", Wire Protocol %s Enabled"
		args = append(args, "Version 2") // for focus color
	}

	return fmt.Sprintf(format, args...)
}

func checkInit() error {
	if setting.Git.HomePath == "" {
		return errors.New("unable to init Git's HomeDir, incorrect initialization of the setting and git modules")
	}
	if DefaultContext != nil {
		log.Warn("git module has been initialized already, duplicate init may work but it's better to fix it")
	}
	return nil
}

// HomeDir is the home dir for git to store the global config file used by Gitea internally
func HomeDir() string {
	if setting.Git.HomePath == "" {
		// strict check, make sure the git module is initialized correctly.
		// attention: when the git module is called in gitea sub-command (serv/hook), the log module might not obviously show messages to users/developers.
		// for example: if there is gitea git hook code calling git.NewCommand before git.InitXxx, the integration test won't show the real failure reasons.
		log.Fatal("Unable to init Git's HomeDir, incorrect initialization of the setting and git modules")
		return ""
	}
	return setting.Git.HomePath
}

// InitSimple initializes git module with a very simple step, no config changes, no global command arguments.
// This method doesn't change anything to filesystem. At the moment, it is only used by some Gitea sub-commands.
func InitSimple(ctx context.Context) error {
	if err := checkInit(); err != nil {
		return err
	}

	DefaultContext = ctx
	globalCommandArgs = nil

	if setting.Git.Timeout.Default > 0 {
		defaultCommandExecutionTimeout = time.Duration(setting.Git.Timeout.Default) * time.Second
	}

	return SetExecutablePath(setting.Git.Path)
}

// InitFull initializes git module with version check and change global variables, sync gitconfig.
// It should only be called once at the beginning of the program initialization (TestMain/GlobalInitInstalled) as this code makes unsynchronized changes to variables.
func InitFull(ctx context.Context) (err error) {
	if err = InitSimple(ctx); err != nil {
		return err
	}

	// when git works with gnupg (commit signing), there should be a stable home for gnupg commands
	if _, ok := os.LookupEnv("GNUPGHOME"); !ok {
		_ = os.Setenv("GNUPGHOME", filepath.Join(HomeDir(), ".gnupg"))
	}

	// Since git wire protocol has been released from git v2.18
	if setting.Git.EnableAutoGitWireProtocol && CheckGitVersionAtLeast("2.18") == nil {
		globalCommandArgs = append(globalCommandArgs, "-c", "protocol.version=2")
	}

	// Explicitly disable credential helper, otherwise Git credentials might leak
	if CheckGitVersionAtLeast("2.9") == nil {
		globalCommandArgs = append(globalCommandArgs, "-c", "credential.helper=")
	}
	DefaultFeatures.SupportProcReceive = CheckGitVersionAtLeast("2.29") == nil
	DefaultFeatures.SupportHashSha256 = CheckGitVersionAtLeast("2.42") == nil && !isGogit
	if DefaultFeatures.SupportHashSha256 {
		SupportedObjectFormats = append(SupportedObjectFormats, Sha256ObjectFormat)
	} else {
		log.Warn("sha256 hash support is disabled - requires Git >= 2.42. Gogit is currently unsupported")
	}

	if setting.LFS.StartServer {
		if CheckGitVersionAtLeast("2.1.2") != nil {
			return errors.New("LFS server support requires Git >= 2.1.2")
		}
		globalCommandArgs = append(globalCommandArgs, "-c", "filter.lfs.required=", "-c", "filter.lfs.smudge=", "-c", "filter.lfs.clean=")
	}

	return syncGitConfig()
}

// syncGitConfig only modifies gitconfig, won't change global variables (otherwise there will be data-race problem)
func syncGitConfig() (err error) {
	if err = os.MkdirAll(HomeDir(), os.ModePerm); err != nil {
		return fmt.Errorf("unable to prepare git home directory %s, err: %w", HomeDir(), err)
	}

	// first, write user's git config options to git config file
	// user config options could be overwritten by builtin values later, because if a value is builtin, it must have some special purposes
	for k, v := range setting.GitConfig.Options {
		if err = configSet(strings.ToLower(k), v); err != nil {
			return err
		}
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
		if err := configSet("fetch.writeCommitGraph", "true"); err != nil {
			return err
		}
	}

	if DefaultFeatures.SupportProcReceive {
		// set support for AGit flow
		if err := configAddNonExist("receive.procReceiveRefs", "refs/for"); err != nil {
			return err
		}
	} else {
		if err := configUnsetAll("receive.procReceiveRefs", "refs/for"); err != nil {
			return err
		}
	}

	// Due to CVE-2022-24765, git now denies access to git directories which are not owned by current user.
	// However, some docker users and samba users find it difficult to configure their systems correctly,
	// so that Gitea's git repositories are owned by the Gitea user.
	// (Possibly Windows Service users - but ownership in this case should really be set correctly on the filesystem.)
	// See issue: https://github.com/go-gitea/gitea/issues/19455
	// As Gitea now always use its internal git config file, and access to the git repositories is managed through Gitea,
	// it is now safe to set "safe.directory=*" for internal usage only.
	// Although this setting is only supported by some new git versions, it is also tolerated by earlier versions
	if err := configAddNonExist("safe.directory", "*"); err != nil {
		return err
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

	// By default partial clones are disabled, enable them from git v2.22
	if !setting.Git.DisablePartialClone && CheckGitVersionAtLeast("2.22") == nil {
		if err = configSet("uploadpack.allowfilter", "true"); err != nil {
			return err
		}
		err = configSet("uploadpack.allowAnySHA1InWant", "true")
	} else {
		if err = configUnsetAll("uploadpack.allowfilter", "true"); err != nil {
			return err
		}
		err = configUnsetAll("uploadpack.allowAnySHA1InWant", "true")
	}

	return err
}

// CheckGitVersionAtLeast check git version is at least the constraint version
func CheckGitVersionAtLeast(atLeast string) error {
	if DefaultFeatures.GitVersion == nil {
		panic("git module is not initialized") // it shouldn't happen
	}
	atLeastVersion, err := version.NewVersion(atLeast)
	if err != nil {
		return err
	}
	if DefaultFeatures.GitVersion.Compare(atLeastVersion) < 0 {
		return fmt.Errorf("installed git binary version %s is not at least %s", DefaultFeatures.GitVersion.Original(), atLeast)
	}
	return nil
}

func checkGitVersionCompatibility(gitVer *version.Version) error {
	badVersions := []struct {
		Version *version.Version
		Reason  string
	}{
		{version.Must(version.NewVersion("2.43.1")), "regression bug of GIT_FLUSH"},
	}
	for _, bad := range badVersions {
		if gitVer.Equal(bad.Version) {
			return errors.New(bad.Reason)
		}
	}
	return nil
}

func configSet(key, value string) error {
	stdout, _, err := NewCommand(DefaultContext, "config", "--global", "--get").AddDynamicArguments(key).RunStdString(nil)
	if err != nil && !IsErrorExitCode(err, 1) {
		return fmt.Errorf("failed to get git config %s, err: %w", key, err)
	}

	currValue := strings.TrimSpace(stdout)
	if currValue == value {
		return nil
	}

	_, _, err = NewCommand(DefaultContext, "config", "--global").AddDynamicArguments(key, value).RunStdString(nil)
	if err != nil {
		return fmt.Errorf("failed to set git global config %s, err: %w", key, err)
	}

	return nil
}

func configSetNonExist(key, value string) error {
	_, _, err := NewCommand(DefaultContext, "config", "--global", "--get").AddDynamicArguments(key).RunStdString(nil)
	if err == nil {
		// already exist
		return nil
	}
	if IsErrorExitCode(err, 1) {
		// not exist, set new config
		_, _, err = NewCommand(DefaultContext, "config", "--global").AddDynamicArguments(key, value).RunStdString(nil)
		if err != nil {
			return fmt.Errorf("failed to set git global config %s, err: %w", key, err)
		}
		return nil
	}

	return fmt.Errorf("failed to get git config %s, err: %w", key, err)
}

func configAddNonExist(key, value string) error {
	_, _, err := NewCommand(DefaultContext, "config", "--global", "--get").AddDynamicArguments(key, regexp.QuoteMeta(value)).RunStdString(nil)
	if err == nil {
		// already exist
		return nil
	}
	if IsErrorExitCode(err, 1) {
		// not exist, add new config
		_, _, err = NewCommand(DefaultContext, "config", "--global", "--add").AddDynamicArguments(key, value).RunStdString(nil)
		if err != nil {
			return fmt.Errorf("failed to add git global config %s, err: %w", key, err)
		}
		return nil
	}
	return fmt.Errorf("failed to get git config %s, err: %w", key, err)
}

func configUnsetAll(key, value string) error {
	_, _, err := NewCommand(DefaultContext, "config", "--global", "--get").AddDynamicArguments(key).RunStdString(nil)
	if err == nil {
		// exist, need to remove
		_, _, err = NewCommand(DefaultContext, "config", "--global", "--unset-all").AddDynamicArguments(key, regexp.QuoteMeta(value)).RunStdString(nil)
		if err != nil {
			return fmt.Errorf("failed to unset git global config %s, err: %w", key, err)
		}
		return nil
	}
	if IsErrorExitCode(err, 1) {
		// not exist
		return nil
	}
	return fmt.Errorf("failed to get git config %s, err: %w", key, err)
}

// Fsck verifies the connectivity and validity of the objects in the database
func Fsck(ctx context.Context, repoPath string, timeout time.Duration, args TrustedCmdArgs) error {
	return NewCommand(ctx, "fsck").AddArguments(args...).Run(&RunOpts{Timeout: timeout, Dir: repoPath})
}
