// Copyright 2014 The Gogs Authors. All rights reserved.
// Copyright 2017 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package setting

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"code.gitea.io/gitea/modules/auth/password/hash"
	"code.gitea.io/gitea/modules/log"
	"code.gitea.io/gitea/modules/user"
	"code.gitea.io/gitea/modules/util"

	ini "gopkg.in/ini.v1"
)

// settings
var (
	// AppVer is the version of the current build of Gitea. It is set in main.go from main.Version.
	AppVer string
	// AppBuiltWith represents a human readable version go runtime build version and build tags. (See main.go formatBuiltWith().)
	AppBuiltWith string
	// AppStartTime store time gitea has started
	AppStartTime time.Time

	// AppPath represents the path to the gitea binary
	AppPath string
	// AppWorkPath is the "working directory" of Gitea. It maps to the environment variable GITEA_WORK_DIR.
	// If that is not set it is the default set here by the linker or failing that the directory of AppPath.
	//
	// AppWorkPath is used as the base path for several other paths.
	AppWorkPath string

	// Global setting objects
	CfgProvider  ConfigProvider
	CustomPath   string // Custom directory path
	CustomConf   string
	PIDFile      = "/run/gitea.pid"
	WritePIDFile bool
	RunMode      string
	RunUser      string
	IsProd       bool
	IsWindows    bool
)

func getAppPath() (string, error) {
	var appPath string
	var err error
	if IsWindows && filepath.IsAbs(os.Args[0]) {
		appPath = filepath.Clean(os.Args[0])
	} else {
		appPath, err = exec.LookPath(os.Args[0])
	}

	if err != nil {
		if !errors.Is(err, exec.ErrDot) {
			return "", err
		}
		appPath, err = filepath.Abs(os.Args[0])
	}
	if err != nil {
		return "", err
	}
	appPath, err = filepath.Abs(appPath)
	if err != nil {
		return "", err
	}
	// Note: we don't use path.Dir here because it does not handle case
	//	which path starts with two "/" in Windows: "//psf/Home/..."
	return strings.ReplaceAll(appPath, "\\", "/"), err
}

func getWorkPath(appPath string) string {
	workPath := AppWorkPath

	if giteaWorkPath, ok := os.LookupEnv("GITEA_WORK_DIR"); ok {
		workPath = giteaWorkPath
	}
	if len(workPath) == 0 {
		i := strings.LastIndex(appPath, "/")
		if i == -1 {
			workPath = appPath
		} else {
			workPath = appPath[:i]
		}
	}
	workPath = strings.ReplaceAll(workPath, "\\", "/")
	if !filepath.IsAbs(workPath) {
		log.Info("Provided work path %s is not absolute - will be made absolute against the current working directory", workPath)

		absPath, err := filepath.Abs(workPath)
		if err != nil {
			log.Error("Unable to absolute %s against the current working directory %v. Will absolute against the AppPath %s", workPath, err, appPath)
			workPath = filepath.Join(appPath, workPath)
		} else {
			workPath = absPath
		}
	}
	return strings.ReplaceAll(workPath, "\\", "/")
}

func init() {
	IsWindows = runtime.GOOS == "windows"
	// We can rely on log.CanColorStdout being set properly because modules/log/console_windows.go comes before modules/setting/setting.go lexicographically
	// By default set this logger at Info - we'll change it later but we need to start with something.
	log.NewLogger(0, "console", "console", fmt.Sprintf(`{"level": "info", "colorize": %t, "stacktraceLevel": "none"}`, log.CanColorStdout))

	var err error
	if AppPath, err = getAppPath(); err != nil {
		log.Fatal("Failed to get app path: %v", err)
	}
	AppWorkPath = getWorkPath(AppPath)
}

func forcePathSeparator(path string) {
	if strings.Contains(path, "\\") {
		log.Fatal("Do not use '\\' or '\\\\' in paths, instead, please use '/' in all places")
	}
}

// IsRunUserMatchCurrentUser returns false if configured run user does not match
// actual user that runs the app. The first return value is the actual user name.
// This check is ignored under Windows since SSH remote login is not the main
// method to login on Windows.
func IsRunUserMatchCurrentUser(runUser string) (string, bool) {
	if IsWindows || SSH.StartBuiltinServer {
		return "", true
	}

	currentUser := user.CurrentUsername()
	return currentUser, runUser == currentUser
}

func createPIDFile(pidPath string) {
	currentPid := os.Getpid()
	if err := os.MkdirAll(filepath.Dir(pidPath), os.ModePerm); err != nil {
		log.Fatal("Failed to create PID folder: %v", err)
	}

	file, err := os.Create(pidPath)
	if err != nil {
		log.Fatal("Failed to create PID file: %v", err)
	}
	defer file.Close()
	if _, err := file.WriteString(strconv.FormatInt(int64(currentPid), 10)); err != nil {
		log.Fatal("Failed to write PID information: %v", err)
	}
}

// SetCustomPathAndConf will set CustomPath and CustomConf with reference to the
// GITEA_CUSTOM environment variable and with provided overrides before stepping
// back to the default
func SetCustomPathAndConf(providedCustom, providedConf, providedWorkPath string) {
	if len(providedWorkPath) != 0 {
		AppWorkPath = filepath.ToSlash(providedWorkPath)
	}
	if giteaCustom, ok := os.LookupEnv("GITEA_CUSTOM"); ok {
		CustomPath = giteaCustom
	}
	if len(providedCustom) != 0 {
		CustomPath = providedCustom
	}
	if len(CustomPath) == 0 {
		CustomPath = path.Join(AppWorkPath, "custom")
	} else if !filepath.IsAbs(CustomPath) {
		CustomPath = path.Join(AppWorkPath, CustomPath)
	}

	if len(providedConf) != 0 {
		CustomConf = providedConf
	}
	if len(CustomConf) == 0 {
		CustomConf = path.Join(CustomPath, "conf/app.ini")
	} else if !filepath.IsAbs(CustomConf) {
		CustomConf = path.Join(CustomPath, CustomConf)
		log.Warn("Using 'custom' directory as relative origin for configuration file: '%s'", CustomConf)
	}
}

// PrepareAppDataPath creates app data directory if necessary
func PrepareAppDataPath() error {
	// FIXME: There are too many calls to MkdirAll in old code. It is incorrect.
	// For example, if someDir=/mnt/vol1/gitea-home/data, if the mount point /mnt/vol1 is not mounted when Gitea runs,
	// then gitea will make new empty directories in /mnt/vol1, all are stored in the root filesystem.
	// The correct behavior should be: creating parent directories is end users' duty. We only create sub-directories in existing parent directories.
	// For quickstart, the parent directories should be created automatically for first startup (eg: a flag or a check of INSTALL_LOCK).
	// Now we can take the first step to do correctly (using Mkdir) in other packages, and prepare the AppDataPath here, then make a refactor in future.

	st, err := os.Stat(AppDataPath)
	if os.IsNotExist(err) {
		err = os.MkdirAll(AppDataPath, os.ModePerm)
		if err != nil {
			return fmt.Errorf("unable to create the APP_DATA_PATH directory: %q, Error: %w", AppDataPath, err)
		}
		return nil
	}

	if err != nil {
		return fmt.Errorf("unable to use APP_DATA_PATH %q. Error: %w", AppDataPath, err)
	}

	if !st.IsDir() /* also works for symlink */ {
		return fmt.Errorf("the APP_DATA_PATH %q is not a directory (or symlink to a directory) and can't be used", AppDataPath)
	}

	return nil
}

// InitProviderFromExistingFile initializes config provider from an existing config file (app.ini)
func InitProviderFromExistingFile() {
	CfgProvider = newFileProviderFromConf(CustomConf, WritePIDFile, false, PIDFile, "")
}

// InitProviderAllowEmpty initializes config provider from file, it's also fine that if the config file (app.ini) doesn't exist
func InitProviderAllowEmpty() {
	CfgProvider = newFileProviderFromConf(CustomConf, WritePIDFile, true, PIDFile, "")
}

// InitProviderAndLoadCommonSettingsForTest initializes config provider and load common setttings for tests
func InitProviderAndLoadCommonSettingsForTest(extraConfigs ...string) {
	CfgProvider = newFileProviderFromConf(CustomConf, WritePIDFile, true, PIDFile, strings.Join(extraConfigs, "\n"))
	loadCommonSettingsFrom(CfgProvider)
	if err := PrepareAppDataPath(); err != nil {
		log.Fatal("Can not prepare APP_DATA_PATH: %v", err)
	}
	// register the dummy hash algorithm function used in the test fixtures
	_ = hash.Register("dummy", hash.NewDummyHasher)

	PasswordHashAlgo, _ = hash.SetDefaultPasswordHashAlgorithm("dummy")
}

// newFileProviderFromConf initializes configuration context.
// NOTE: do not print any log except error.
func newFileProviderFromConf(customConf string, writePIDFile, allowEmpty bool, pidFile, extraConfig string) *ini.File {
	cfg := ini.Empty()

	if writePIDFile && len(pidFile) > 0 {
		createPIDFile(pidFile)
	}

	isFile, err := util.IsFile(customConf)
	if err != nil {
		log.Error("Unable to check if %s is a file. Error: %v", customConf, err)
	}
	if isFile {
		if err := cfg.Append(customConf); err != nil {
			log.Fatal("Failed to load custom conf '%s': %v", customConf, err)
		}
	} else if !allowEmpty {
		log.Fatal("Unable to find configuration file: %q.\nEnsure you are running in the correct environment or set the correct configuration file with -c.", CustomConf)
	} // else: no config file, a config file might be created at CustomConf later (might not)

	if extraConfig != "" {
		if err = cfg.Append([]byte(extraConfig)); err != nil {
			log.Fatal("Unable to append more config: %v", err)
		}
	}

	cfg.NameMapper = ini.SnackCase
	return cfg
}

// LoadCommonSettings loads common configurations from a configuration provider.
func LoadCommonSettings() {
	loadCommonSettingsFrom(CfgProvider)
}

// loadCommonSettingsFrom loads common configurations from a configuration provider.
func loadCommonSettingsFrom(cfg ConfigProvider) {
	// WARNNING: don't change the sequence except you know what you are doing.
	loadRunModeFrom(cfg)
	loadLogFrom(cfg)
	loadServerFrom(cfg)
	loadSSHFrom(cfg)
	loadOAuth2From(cfg)
	loadSecurityFrom(cfg)
	loadAttachmentFrom(cfg)
	loadLFSFrom(cfg)
	loadTimeFrom(cfg)
	loadRepositoryFrom(cfg)
	loadPictureFrom(cfg)
	loadPackagesFrom(cfg)
	loadActionsFrom(cfg)
	loadUIFrom(cfg)
	loadAdminFrom(cfg)
	loadAPIFrom(cfg)
	loadMetricsFrom(cfg)
	loadCamoFrom(cfg)
	loadI18nFrom(cfg)
	loadGitFrom(cfg)
	loadMirrorFrom(cfg)
	loadMarkupFrom(cfg)
	loadOtherFrom(cfg)
}

func loadRunModeFrom(rootCfg ConfigProvider) {
	rootSec := rootCfg.Section("")
	RunUser = rootSec.Key("RUN_USER").MustString(user.CurrentUsername())
	// The following is a purposefully undocumented option. Please do not run Gitea as root. It will only cause future headaches.
	// Please don't use root as a bandaid to "fix" something that is broken, instead the broken thing should instead be fixed properly.
	unsafeAllowRunAsRoot := rootSec.Key("I_AM_BEING_UNSAFE_RUNNING_AS_ROOT").MustBool(false)
	RunMode = os.Getenv("GITEA_RUN_MODE")
	if RunMode == "" {
		RunMode = rootSec.Key("RUN_MODE").MustString("prod")
	}
	IsProd = strings.EqualFold(RunMode, "prod")
	// Does not check run user when the install lock is off.
	installLock := rootCfg.Section("security").Key("INSTALL_LOCK").MustBool(false)
	if installLock {
		currentUser, match := IsRunUserMatchCurrentUser(RunUser)
		if !match {
			log.Fatal("Expect user '%s' but current user is: %s", RunUser, currentUser)
		}
	}

	// check if we run as root
	if os.Getuid() == 0 {
		if !unsafeAllowRunAsRoot {
			// Special thanks to VLC which inspired the wording of this messaging.
			log.Fatal("Gitea is not supposed to be run as root. Sorry. If you need to use privileged TCP ports please instead use setcap and the `cap_net_bind_service` permission")
		}
		log.Critical("You are running Gitea using the root user, and have purposely chosen to skip built-in protections around this. You have been warned against this.")
	}
}

// CreateOrAppendToCustomConf creates or updates the custom config.
// Use the callback to set individual values.
func CreateOrAppendToCustomConf(purpose string, callback func(cfg *ini.File)) {
	if CustomConf == "" {
		log.Error("Custom config path must not be empty")
		return
	}

	cfg := ini.Empty()
	isFile, err := util.IsFile(CustomConf)
	if err != nil {
		log.Error("Unable to check if %s is a file. Error: %v", CustomConf, err)
	}
	if isFile {
		if err := cfg.Append(CustomConf); err != nil {
			log.Error("failed to load custom conf %s: %v", CustomConf, err)
			return
		}
	}

	callback(cfg)

	if err := os.MkdirAll(filepath.Dir(CustomConf), os.ModePerm); err != nil {
		log.Fatal("failed to create '%s': %v", CustomConf, err)
		return
	}
	if err := cfg.SaveTo(CustomConf); err != nil {
		log.Fatal("error saving to custom config: %v", err)
	}
	log.Info("Settings for %s saved to: %q", purpose, CustomConf)

	// Change permissions to be more restrictive
	fi, err := os.Stat(CustomConf)
	if err != nil {
		log.Error("Failed to determine current conf file permissions: %v", err)
		return
	}

	if fi.Mode().Perm() > 0o600 {
		if err = os.Chmod(CustomConf, 0o600); err != nil {
			log.Warn("Failed changing conf file permissions to -rw-------. Consider changing them manually.")
		}
	}
}

// LoadSettings initializes the settings for normal start up
func LoadSettings() {
	LoadDBSetting()
	loadServiceFrom(CfgProvider)
	loadOAuth2ClientFrom(CfgProvider)
	InitLogs(false)
	loadCacheFrom(CfgProvider)
	loadSessionFrom(CfgProvider)
	loadCorsFrom(CfgProvider)
	loadMailsFrom(CfgProvider)
	loadProxyFrom(CfgProvider)
	loadWebhookFrom(CfgProvider)
	loadMigrationsFrom(CfgProvider)
	loadIndexerFrom(CfgProvider)
	loadTaskFrom(CfgProvider)
	LoadQueueSettings()
	loadProjectFrom(CfgProvider)
	loadMimeTypeMapFrom(CfgProvider)
	loadFederationFrom(CfgProvider)
}

// LoadSettingsForInstall initializes the settings for install
func LoadSettingsForInstall() {
	LoadDBSetting()
	loadServiceFrom(CfgProvider)
	loadMailerFrom(CfgProvider)
}
