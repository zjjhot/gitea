// Copyright 2019 The Gitea Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package repository

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"code.gitea.io/gitea/models"
	repo_model "code.gitea.io/gitea/models/repo"
	user_model "code.gitea.io/gitea/models/user"
	"code.gitea.io/gitea/modules/git"
	"code.gitea.io/gitea/modules/log"
	"code.gitea.io/gitea/modules/util"

	"github.com/huandu/xstrings"
)

type transformer struct {
	Name      string
	Transform func(string) string
}

type expansion struct {
	Name         string
	Value        string
	Transformers []transformer
}

var defaultTransformers = []transformer{
	{Name: "SNAKE", Transform: xstrings.ToSnakeCase},
	{Name: "KEBAB", Transform: xstrings.ToKebabCase},
	{Name: "CAMEL", Transform: func(str string) string {
		return xstrings.FirstRuneToLower(xstrings.ToCamelCase(str))
	}},
	{Name: "PASCAL", Transform: xstrings.ToCamelCase},
	{Name: "LOWER", Transform: strings.ToLower},
	{Name: "UPPER", Transform: strings.ToUpper},
	{Name: "TITLE", Transform: strings.Title},
}

func generateExpansion(src string, templateRepo, generateRepo *repo_model.Repository) string {
	expansions := []expansion{
		{Name: "REPO_NAME", Value: generateRepo.Name, Transformers: defaultTransformers},
		{Name: "TEMPLATE_NAME", Value: templateRepo.Name, Transformers: defaultTransformers},
		{Name: "REPO_DESCRIPTION", Value: generateRepo.Description, Transformers: nil},
		{Name: "TEMPLATE_DESCRIPTION", Value: templateRepo.Description, Transformers: nil},
		{Name: "REPO_OWNER", Value: generateRepo.OwnerName, Transformers: defaultTransformers},
		{Name: "TEMPLATE_OWNER", Value: templateRepo.OwnerName, Transformers: defaultTransformers},
		{Name: "REPO_LINK", Value: generateRepo.Link(), Transformers: nil},
		{Name: "TEMPLATE_LINK", Value: templateRepo.Link(), Transformers: nil},
		{Name: "REPO_HTTPS_URL", Value: generateRepo.CloneLink().HTTPS, Transformers: nil},
		{Name: "TEMPLATE_HTTPS_URL", Value: templateRepo.CloneLink().HTTPS, Transformers: nil},
		{Name: "REPO_SSH_URL", Value: generateRepo.CloneLink().SSH, Transformers: nil},
		{Name: "TEMPLATE_SSH_URL", Value: templateRepo.CloneLink().SSH, Transformers: nil},
	}

	expansionMap := make(map[string]string)
	for _, e := range expansions {
		expansionMap[e.Name] = e.Value
		for _, tr := range e.Transformers {
			expansionMap[fmt.Sprintf("%s_%s", e.Name, tr.Name)] = tr.Transform(e.Value)
		}
	}

	return os.Expand(src, func(key string) string {
		if expansion, ok := expansionMap[key]; ok {
			return expansion
		}
		return key
	})
}

func checkGiteaTemplate(tmpDir string) (*models.GiteaTemplate, error) {
	gtPath := filepath.Join(tmpDir, ".gitea", "template")
	if _, err := os.Stat(gtPath); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	content, err := os.ReadFile(gtPath)
	if err != nil {
		return nil, err
	}

	gt := &models.GiteaTemplate{
		Path:    gtPath,
		Content: content,
	}

	return gt, nil
}

func generateRepoCommit(ctx context.Context, repo, templateRepo, generateRepo *repo_model.Repository, tmpDir string) error {
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
	templateRepoPath := templateRepo.RepoPath()
	if err := git.Clone(ctx, templateRepoPath, tmpDir, git.CloneRepoOptions{
		Depth:  1,
		Branch: templateRepo.DefaultBranch,
	}); err != nil {
		return fmt.Errorf("git clone: %v", err)
	}

	if err := util.RemoveAll(path.Join(tmpDir, ".git")); err != nil {
		return fmt.Errorf("remove git dir: %v", err)
	}

	// Variable expansion
	gt, err := checkGiteaTemplate(tmpDir)
	if err != nil {
		return fmt.Errorf("checkGiteaTemplate: %v", err)
	}

	if gt != nil {
		if err := util.Remove(gt.Path); err != nil {
			return fmt.Errorf("remove .giteatemplate: %v", err)
		}

		// Avoid walking tree if there are no globs
		if len(gt.Globs()) > 0 {
			tmpDirSlash := strings.TrimSuffix(filepath.ToSlash(tmpDir), "/") + "/"
			if err := filepath.Walk(tmpDirSlash, func(path string, info os.FileInfo, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}

				if info.IsDir() {
					return nil
				}

				base := strings.TrimPrefix(filepath.ToSlash(path), tmpDirSlash)
				for _, g := range gt.Globs() {
					if g.Match(base) {
						content, err := os.ReadFile(path)
						if err != nil {
							return err
						}

						if err := os.WriteFile(path,
							[]byte(generateExpansion(string(content), templateRepo, generateRepo)),
							0o644); err != nil {
							return err
						}
						break
					}
				}
				return nil
			}); err != nil {
				return err
			}
		}
	}

	if err := git.InitRepository(ctx, tmpDir, false); err != nil {
		return err
	}

	repoPath := repo.RepoPath()
	if stdout, err := git.NewCommand(ctx, "remote", "add", "origin", repoPath).
		SetDescription(fmt.Sprintf("generateRepoCommit (git remote add): %s to %s", templateRepoPath, tmpDir)).
		RunInDirWithEnv(tmpDir, env); err != nil {
		log.Error("Unable to add %v as remote origin to temporary repo to %s: stdout %s\nError: %v", repo, tmpDir, stdout, err)
		return fmt.Errorf("git remote add: %v", err)
	}

	return initRepoCommit(ctx, tmpDir, repo, repo.Owner, templateRepo.DefaultBranch)
}

func generateGitContent(ctx context.Context, repo, templateRepo, generateRepo *repo_model.Repository) (err error) {
	tmpDir, err := os.MkdirTemp(os.TempDir(), "gitea-"+repo.Name)
	if err != nil {
		return fmt.Errorf("Failed to create temp dir for repository %s: %v", repo.RepoPath(), err)
	}

	defer func() {
		if err := util.RemoveAll(tmpDir); err != nil {
			log.Error("RemoveAll: %v", err)
		}
	}()

	if err = generateRepoCommit(ctx, repo, templateRepo, generateRepo, tmpDir); err != nil {
		return fmt.Errorf("generateRepoCommit: %v", err)
	}

	// re-fetch repo
	if repo, err = repo_model.GetRepositoryByIDCtx(ctx, repo.ID); err != nil {
		return fmt.Errorf("getRepositoryByID: %v", err)
	}

	repo.DefaultBranch = templateRepo.DefaultBranch
	gitRepo, err := git.OpenRepositoryCtx(ctx, repo.RepoPath())
	if err != nil {
		return fmt.Errorf("openRepository: %v", err)
	}
	defer gitRepo.Close()
	if err = gitRepo.SetDefaultBranch(repo.DefaultBranch); err != nil {
		return fmt.Errorf("setDefaultBranch: %v", err)
	}
	if err = models.UpdateRepositoryCtx(ctx, repo, false); err != nil {
		return fmt.Errorf("updateRepository: %v", err)
	}

	return nil
}

// GenerateGitContent generates git content from a template repository
func GenerateGitContent(ctx context.Context, templateRepo, generateRepo *repo_model.Repository) error {
	if err := generateGitContent(ctx, generateRepo, templateRepo, generateRepo); err != nil {
		return err
	}

	if err := models.UpdateRepoSize(ctx, generateRepo); err != nil {
		return fmt.Errorf("failed to update size for repository: %v", err)
	}

	if err := models.CopyLFS(ctx, generateRepo, templateRepo); err != nil {
		return fmt.Errorf("failed to copy LFS: %v", err)
	}
	return nil
}

// GenerateRepository generates a repository from a template
func GenerateRepository(ctx context.Context, doer, owner *user_model.User, templateRepo *repo_model.Repository, opts models.GenerateRepoOptions) (_ *repo_model.Repository, err error) {
	generateRepo := &repo_model.Repository{
		OwnerID:       owner.ID,
		Owner:         owner,
		OwnerName:     owner.Name,
		Name:          opts.Name,
		LowerName:     strings.ToLower(opts.Name),
		Description:   opts.Description,
		IsPrivate:     opts.Private,
		IsEmpty:       !opts.GitContent || templateRepo.IsEmpty,
		IsFsckEnabled: templateRepo.IsFsckEnabled,
		TemplateID:    templateRepo.ID,
		TrustModel:    templateRepo.TrustModel,
	}

	if err = models.CreateRepository(ctx, doer, owner, generateRepo, false); err != nil {
		return nil, err
	}

	repoPath := generateRepo.RepoPath()
	isExist, err := util.IsExist(repoPath)
	if err != nil {
		log.Error("Unable to check if %s exists. Error: %v", repoPath, err)
		return nil, err
	}
	if isExist {
		return nil, repo_model.ErrRepoFilesAlreadyExist{
			Uname: generateRepo.OwnerName,
			Name:  generateRepo.Name,
		}
	}

	if err = checkInitRepository(ctx, owner.Name, generateRepo.Name); err != nil {
		return generateRepo, err
	}

	if err = models.CheckDaemonExportOK(ctx, generateRepo); err != nil {
		return generateRepo, fmt.Errorf("checkDaemonExportOK: %v", err)
	}

	if stdout, err := git.NewCommand(ctx, "update-server-info").
		SetDescription(fmt.Sprintf("GenerateRepository(git update-server-info): %s", repoPath)).
		RunInDir(repoPath); err != nil {
		log.Error("GenerateRepository(git update-server-info) in %v: Stdout: %s\nError: %v", generateRepo, stdout, err)
		return generateRepo, fmt.Errorf("error in GenerateRepository(git update-server-info): %v", err)
	}

	return generateRepo, nil
}
