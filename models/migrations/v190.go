// Copyright 2021 The Gitea Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package migrations

import (
	"fmt"

	"xorm.io/xorm"
)

func addAgitFlowPullRequest(x *xorm.Engine) error {
	type PullRequestFlow int

	type PullRequest struct {
		Flow PullRequestFlow `xorm:"NOT NULL DEFAULT 0"`
	}

	if err := x.Sync2(new(PullRequest)); err != nil {
		return fmt.Errorf("sync2: %w", err)
	}
	return nil
}
