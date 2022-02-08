// Copyright 2019 The Gitea Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package repository

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"code.gitea.io/gitea/models"
	repo_model "code.gitea.io/gitea/models/repo"
	user_model "code.gitea.io/gitea/models/user"
	"code.gitea.io/gitea/modules/git"
	"code.gitea.io/gitea/modules/log"
	"code.gitea.io/gitea/modules/setting"
	"code.gitea.io/gitea/modules/util"
	asymkey_service "code.gitea.io/gitea/services/asymkey"

	"github.com/unknwon/com"
)

func prepareRepoCommit(ctx context.Context, repo *repo_model.Repository, tmpDir, repoPath string, opts models.CreateRepoOptions) error {
	commitTimeStr := time.Now().Format(time.RFC3339)
	authorSig := repo.Owner.NewGitSig()

	// Because this may call hooks we should pass in the environment
	env := append(os.Environ(),
		"GIT_AUTHOR_NAME="+authorSig.Name,
		"GIT_AUTHOR_EMAIL="+authorSig.Email,
		"GIT_AUTHOR_DATE="+commitTimeStr,
		"GIT_COMMITTER_NAME="+authorSig.Name,
		"GIT_COMMITTER_EMAIL="+authorSig.Email,
		"GIT_COMMITTER_DATE="+commitTimeStr,
	)

	// Clone to temporary path and do the init commit.
	if stdout, err := git.NewCommand(ctx, "clone", repoPath, tmpDir).
		SetDescription(fmt.Sprintf("prepareRepoCommit (git clone): %s to %s", repoPath, tmpDir)).
		RunInDirWithEnv("", env); err != nil {
		log.Error("Failed to clone from %v into %s: stdout: %s\nError: %v", repo, tmpDir, stdout, err)
		return fmt.Errorf("git clone: %v", err)
	}

	// README
	data, err := models.GetRepoInitFile("readme", opts.Readme)
	if err != nil {
		return fmt.Errorf("GetRepoInitFile[%s]: %v", opts.Readme, err)
	}

	cloneLink := repo.CloneLink()
	match := map[string]string{
		"Name":           repo.Name,
		"Description":    repo.Description,
		"CloneURL.SSH":   cloneLink.SSH,
		"CloneURL.HTTPS": cloneLink.HTTPS,
		"OwnerName":      repo.OwnerName,
	}
	if err = os.WriteFile(filepath.Join(tmpDir, "README.md"),
		[]byte(com.Expand(string(data), match)), 0o644); err != nil {
		return fmt.Errorf("write README.md: %v", err)
	}

	// .gitignore
	if len(opts.Gitignores) > 0 {
		var buf bytes.Buffer
		names := strings.Split(opts.Gitignores, ",")
		for _, name := range names {
			data, err = models.GetRepoInitFile("gitignore", name)
			if err != nil {
				return fmt.Errorf("GetRepoInitFile[%s]: %v", name, err)
			}
			buf.WriteString("# ---> " + name + "\n")
			buf.Write(data)
			buf.WriteString("\n")
		}

		if buf.Len() > 0 {
			if err = os.WriteFile(filepath.Join(tmpDir, ".gitignore"), buf.Bytes(), 0o644); err != nil {
				return fmt.Errorf("write .gitignore: %v", err)
			}
		}
	}

	// LICENSE
	if len(opts.License) > 0 {
		data, err = models.GetRepoInitFile("license", opts.License)
		if err != nil {
			return fmt.Errorf("GetRepoInitFile[%s]: %v", opts.License, err)
		}

		if err = os.WriteFile(filepath.Join(tmpDir, "LICENSE"), data, 0o644); err != nil {
			return fmt.Errorf("write LICENSE: %v", err)
		}
	}

	return nil
}

// initRepoCommit temporarily changes with work directory.
func initRepoCommit(ctx context.Context, tmpPath string, repo *repo_model.Repository, u *user_model.User, defaultBranch string) (err error) {
	commitTimeStr := time.Now().Format(time.RFC3339)

	sig := u.NewGitSig()
	// Because this may call hooks we should pass in the environment
	env := append(os.Environ(),
		"GIT_AUTHOR_NAME="+sig.Name,
		"GIT_AUTHOR_EMAIL="+sig.Email,
		"GIT_AUTHOR_DATE="+commitTimeStr,
		"GIT_COMMITTER_DATE="+commitTimeStr,
	)
	committerName := sig.Name
	committerEmail := sig.Email

	if stdout, err := git.NewCommand(ctx, "add", "--all").
		SetDescription(fmt.Sprintf("initRepoCommit (git add): %s", tmpPath)).
		RunInDir(tmpPath); err != nil {
		log.Error("git add --all failed: Stdout: %s\nError: %v", stdout, err)
		return fmt.Errorf("git add --all: %v", err)
	}

	err = git.LoadGitVersion()
	if err != nil {
		return fmt.Errorf("Unable to get git version: %v", err)
	}

	args := []string{
		"commit", fmt.Sprintf("--author='%s <%s>'", sig.Name, sig.Email),
		"-m", "Initial commit",
	}

	if git.CheckGitVersionAtLeast("1.7.9") == nil {
		sign, keyID, signer, _ := asymkey_service.SignInitialCommit(ctx, tmpPath, u)
		if sign {
			args = append(args, "-S"+keyID)

			if repo.GetTrustModel() == repo_model.CommitterTrustModel || repo.GetTrustModel() == repo_model.CollaboratorCommitterTrustModel {
				// need to set the committer to the KeyID owner
				committerName = signer.Name
				committerEmail = signer.Email
			}
		} else if git.CheckGitVersionAtLeast("2.0.0") == nil {
			args = append(args, "--no-gpg-sign")
		}
	}

	env = append(env,
		"GIT_COMMITTER_NAME="+committerName,
		"GIT_COMMITTER_EMAIL="+committerEmail,
	)

	if stdout, err := git.NewCommand(ctx, args...).
		SetDescription(fmt.Sprintf("initRepoCommit (git commit): %s", tmpPath)).
		RunInDirWithEnv(tmpPath, env); err != nil {
		log.Error("Failed to commit: %v: Stdout: %s\nError: %v", args, stdout, err)
		return fmt.Errorf("git commit: %v", err)
	}

	if len(defaultBranch) == 0 {
		defaultBranch = setting.Repository.DefaultBranch
	}

	if stdout, err := git.NewCommand(ctx, "push", "origin", "HEAD:"+defaultBranch).
		SetDescription(fmt.Sprintf("initRepoCommit (git push): %s", tmpPath)).
		RunInDirWithEnv(tmpPath, models.InternalPushingEnvironment(u, repo)); err != nil {
		log.Error("Failed to push back to HEAD: Stdout: %s\nError: %v", stdout, err)
		return fmt.Errorf("git push: %v", err)
	}

	return nil
}

func checkInitRepository(ctx context.Context, owner, name string) (err error) {
	// Somehow the directory could exist.
	repoPath := repo_model.RepoPath(owner, name)
	isExist, err := util.IsExist(repoPath)
	if err != nil {
		log.Error("Unable to check if %s exists. Error: %v", repoPath, err)
		return err
	}
	if isExist {
		return repo_model.ErrRepoFilesAlreadyExist{
			Uname: owner,
			Name:  name,
		}
	}

	// Init git bare new repository.
	if err = git.InitRepository(ctx, repoPath, true); err != nil {
		return fmt.Errorf("git.InitRepository: %v", err)
	} else if err = createDelegateHooks(repoPath); err != nil {
		return fmt.Errorf("createDelegateHooks: %v", err)
	}
	return nil
}

// InitRepository initializes README and .gitignore if needed.
func initRepository(ctx context.Context, repoPath string, u *user_model.User, repo *repo_model.Repository, opts models.CreateRepoOptions) (err error) {
	if err = checkInitRepository(ctx, repo.OwnerName, repo.Name); err != nil {
		return err
	}

	// Initialize repository according to user's choice.
	if opts.AutoInit {
		tmpDir, err := os.MkdirTemp(os.TempDir(), "gitea-"+repo.Name)
		if err != nil {
			return fmt.Errorf("Failed to create temp dir for repository %s: %v", repo.RepoPath(), err)
		}
		defer func() {
			if err := util.RemoveAll(tmpDir); err != nil {
				log.Warn("Unable to remove temporary directory: %s: Error: %v", tmpDir, err)
			}
		}()

		if err = prepareRepoCommit(ctx, repo, tmpDir, repoPath, opts); err != nil {
			return fmt.Errorf("prepareRepoCommit: %v", err)
		}

		// Apply changes and commit.
		if err = initRepoCommit(ctx, tmpDir, repo, u, opts.DefaultBranch); err != nil {
			return fmt.Errorf("initRepoCommit: %v", err)
		}
	}

	// Re-fetch the repository from database before updating it (else it would
	// override changes that were done earlier with sql)
	if repo, err = repo_model.GetRepositoryByIDCtx(ctx, repo.ID); err != nil {
		return fmt.Errorf("getRepositoryByID: %v", err)
	}

	if !opts.AutoInit {
		repo.IsEmpty = true
	}

	repo.DefaultBranch = setting.Repository.DefaultBranch

	if len(opts.DefaultBranch) > 0 {
		repo.DefaultBranch = opts.DefaultBranch
		gitRepo, err := git.OpenRepositoryCtx(ctx, repo.RepoPath())
		if err != nil {
			return fmt.Errorf("openRepository: %v", err)
		}
		defer gitRepo.Close()
		if err = gitRepo.SetDefaultBranch(repo.DefaultBranch); err != nil {
			return fmt.Errorf("setDefaultBranch: %v", err)
		}
	}

	if err = models.UpdateRepositoryCtx(ctx, repo, false); err != nil {
		return fmt.Errorf("updateRepository: %v", err)
	}

	return nil
}
