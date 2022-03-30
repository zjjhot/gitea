// Copyright 2019 The Gitea Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package models

import (
	"bufio"
	"bytes"
	"context"
	"strings"

	"code.gitea.io/gitea/models/db"
	repo_model "code.gitea.io/gitea/models/repo"
	"code.gitea.io/gitea/models/webhook"
	"code.gitea.io/gitea/modules/log"

	"github.com/gobwas/glob"
)

// GenerateRepoOptions contains the template units to generate
type GenerateRepoOptions struct {
	Name          string
	DefaultBranch string
	Description   string
	Private       bool
	GitContent    bool
	Topics        bool
	GitHooks      bool
	Webhooks      bool
	Avatar        bool
	IssueLabels   bool
}

// IsValid checks whether at least one option is chosen for generation
func (gro GenerateRepoOptions) IsValid() bool {
	return gro.GitContent || gro.Topics || gro.GitHooks || gro.Webhooks || gro.Avatar || gro.IssueLabels // or other items as they are added
}

// GiteaTemplate holds information about a .gitea/template file
type GiteaTemplate struct {
	Path    string
	Content []byte

	globs []glob.Glob
}

// Globs parses the .gitea/template globs or returns them if they were already parsed
func (gt GiteaTemplate) Globs() []glob.Glob {
	if gt.globs != nil {
		return gt.globs
	}

	gt.globs = make([]glob.Glob, 0)
	scanner := bufio.NewScanner(bytes.NewReader(gt.Content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		g, err := glob.Compile(line, '/')
		if err != nil {
			log.Info("Invalid glob expression '%s' (skipped): %v", line, err)
			continue
		}
		gt.globs = append(gt.globs, g)
	}
	return gt.globs
}

// GenerateWebhooks generates webhooks from a template repository
func GenerateWebhooks(ctx context.Context, templateRepo, generateRepo *repo_model.Repository) error {
	templateWebhooks, err := webhook.ListWebhooksByOpts(&webhook.ListWebhookOptions{RepoID: templateRepo.ID})
	if err != nil {
		return err
	}

	for _, templateWebhook := range templateWebhooks {
		generateWebhook := &webhook.Webhook{
			RepoID:      generateRepo.ID,
			URL:         templateWebhook.URL,
			HTTPMethod:  templateWebhook.HTTPMethod,
			ContentType: templateWebhook.ContentType,
			Secret:      templateWebhook.Secret,
			HookEvent:   templateWebhook.HookEvent,
			IsActive:    templateWebhook.IsActive,
			Type:        templateWebhook.Type,
			OrgID:       templateWebhook.OrgID,
			Events:      templateWebhook.Events,
			Meta:        templateWebhook.Meta,
		}
		if err := webhook.CreateWebhook(ctx, generateWebhook); err != nil {
			return err
		}
	}
	return nil
}

// GenerateIssueLabels generates issue labels from a template repository
func GenerateIssueLabels(ctx context.Context, templateRepo, generateRepo *repo_model.Repository) error {
	templateLabels, err := getLabelsByRepoID(db.GetEngine(ctx), templateRepo.ID, "", db.ListOptions{})
	if err != nil {
		return err
	}

	for _, templateLabel := range templateLabels {
		generateLabel := &Label{
			RepoID:      generateRepo.ID,
			Name:        templateLabel.Name,
			Description: templateLabel.Description,
			Color:       templateLabel.Color,
		}
		if err := db.Insert(ctx, generateLabel); err != nil {
			return err
		}
	}
	return nil
}
