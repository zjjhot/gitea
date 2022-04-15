// Copyright 2020 The Gitea Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package repo

import (
	"path/filepath"
	"testing"

	"code.gitea.io/gitea/models/unittest"
)

func TestMain(m *testing.M) {
	unittest.MainTest(m, &unittest.TestOptions{
		GiteaRootPath: filepath.Join("..", ".."),
		FixtureFiles: []string{
			"attachment.yml",
			"repo_archiver.yml",
			"repository.yml",
			"repo_unit.yml",
			"repo_indexer_status.yml",
			"repo_redirect.yml",
			"watch.yml",
			"star.yml",
			"topic.yml",
			"repo_topic.yml",
			"user.yml",
		},
	})
}
