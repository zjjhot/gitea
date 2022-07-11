// Copyright 2016 The Gitea Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package routers

import (
	"context"
	"reflect"
	"runtime"

	"code.gitea.io/gitea/models"
	asymkey_model "code.gitea.io/gitea/models/asymkey"
	"code.gitea.io/gitea/modules/appstate"
	"code.gitea.io/gitea/modules/cache"
	"code.gitea.io/gitea/modules/eventsource"
	"code.gitea.io/gitea/modules/git"
	"code.gitea.io/gitea/modules/highlight"
	code_indexer "code.gitea.io/gitea/modules/indexer/code"
	issue_indexer "code.gitea.io/gitea/modules/indexer/issues"
	stats_indexer "code.gitea.io/gitea/modules/indexer/stats"
	"code.gitea.io/gitea/modules/log"
	"code.gitea.io/gitea/modules/markup"
	"code.gitea.io/gitea/modules/markup/external"
	"code.gitea.io/gitea/modules/notification"
	"code.gitea.io/gitea/modules/setting"
	"code.gitea.io/gitea/modules/ssh"
	"code.gitea.io/gitea/modules/storage"
	"code.gitea.io/gitea/modules/svg"
	"code.gitea.io/gitea/modules/translation"
	"code.gitea.io/gitea/modules/util"
	"code.gitea.io/gitea/modules/web"
	packages_router "code.gitea.io/gitea/routers/api/packages"
	apiv1 "code.gitea.io/gitea/routers/api/v1"
	"code.gitea.io/gitea/routers/common"
	"code.gitea.io/gitea/routers/private"
	web_routers "code.gitea.io/gitea/routers/web"
	"code.gitea.io/gitea/services/auth"
	"code.gitea.io/gitea/services/auth/source/oauth2"
	"code.gitea.io/gitea/services/automerge"
	"code.gitea.io/gitea/services/cron"
	"code.gitea.io/gitea/services/mailer"
	repo_migrations "code.gitea.io/gitea/services/migrations"
	mirror_service "code.gitea.io/gitea/services/mirror"
	pull_service "code.gitea.io/gitea/services/pull"
	repo_service "code.gitea.io/gitea/services/repository"
	"code.gitea.io/gitea/services/repository/archiver"
	"code.gitea.io/gitea/services/task"
	"code.gitea.io/gitea/services/webhook"
)

func mustInit(fn func() error) {
	err := fn()
	if err != nil {
		ptr := reflect.ValueOf(fn).Pointer()
		fi := runtime.FuncForPC(ptr)
		log.Fatal("%s failed: %v", fi.Name(), err)
	}
}

func mustInitCtx(ctx context.Context, fn func(ctx context.Context) error) {
	err := fn(ctx)
	if err != nil {
		ptr := reflect.ValueOf(fn).Pointer()
		fi := runtime.FuncForPC(ptr)
		log.Fatal("%s(ctx) failed: %v", fi.Name(), err)
	}
}

// InitGitServices init new services for git, this is also called in `contrib/pr/checkout.go`
func InitGitServices() {
	setting.NewServices()
	mustInit(storage.Init)
	mustInit(repo_service.Init)
}

func syncAppPathForGit(ctx context.Context) error {
	runtimeState := new(appstate.RuntimeState)
	if err := appstate.AppState.Get(runtimeState); err != nil {
		return err
	}
	if runtimeState.LastAppPath != setting.AppPath {
		log.Info("AppPath changed from '%s' to '%s'", runtimeState.LastAppPath, setting.AppPath)

		log.Info("re-sync repository hooks ...")
		mustInitCtx(ctx, repo_service.SyncRepositoryHooks)

		log.Info("re-write ssh public keys ...")
		mustInit(asymkey_model.RewriteAllPublicKeys)

		runtimeState.LastAppPath = setting.AppPath
		return appstate.AppState.Set(runtimeState)
	}
	return nil
}

// GlobalInitInstalled is for global installed configuration.
func GlobalInitInstalled(ctx context.Context) {
	if !setting.InstallLock {
		log.Fatal("Gitea is not installed")
	}

	mustInitCtx(ctx, git.InitOnceWithSync)
	log.Info("Git Version: %s (home: %s)", git.VersionInfo(), git.HomeDir())

	git.CheckLFSVersion()
	log.Info("AppPath: %s", setting.AppPath)
	log.Info("AppWorkPath: %s", setting.AppWorkPath)
	log.Info("Custom path: %s", setting.CustomPath)
	log.Info("Log path: %s", setting.LogRootPath)
	log.Info("Configuration file: %s", setting.CustomConf)
	log.Info("Run Mode: %s", util.ToTitleCase(setting.RunMode))

	// Setup i18n
	translation.InitLocales()

	setting.NewServices()
	mustInit(storage.Init)

	mailer.NewContext()
	mustInit(cache.NewContext)
	notification.NewContext()
	mustInit(archiver.Init)

	highlight.NewContext()
	external.RegisterRenderers()
	markup.Init()

	if setting.EnableSQLite3 {
		log.Info("SQLite3 support is enabled")
	} else if setting.Database.UseSQLite3 {
		log.Fatal("SQLite3 support is disabled, but it is used for database setting. Please get or build a Gitea release with SQLite3 support.")
	}

	mustInitCtx(ctx, common.InitDBEngine)
	log.Info("ORM engine initialization successful!")
	mustInit(appstate.Init)
	mustInit(oauth2.Init)

	models.NewRepoContext()
	mustInit(repo_service.Init)

	// Booting long running goroutines.
	cron.NewContext(ctx)
	issue_indexer.InitIssueIndexer(false)
	code_indexer.Init()
	mustInit(stats_indexer.Init)

	mirror_service.InitSyncMirrors()
	mustInit(webhook.Init)
	mustInit(pull_service.Init)
	mustInit(automerge.Init)
	mustInit(task.Init)
	mustInit(repo_migrations.Init)
	eventsource.GetManager().Init()

	mustInitCtx(ctx, syncAppPathForGit)

	mustInit(ssh.Init)

	auth.Init()
	svg.Init()
}

// NormalRoutes represents non install routes
func NormalRoutes() *web.Route {
	r := web.NewRoute()
	for _, middle := range common.Middlewares() {
		r.Use(middle)
	}

	r.Mount("/", web_routers.Routes())
	r.Mount("/api/v1", apiv1.Routes())
	r.Mount("/api/internal", private.Routes())
	if setting.Packages.Enabled {
		r.Mount("/api/packages", packages_router.Routes())
		r.Mount("/v2", packages_router.ContainerRoutes())
	}
	return r
}
