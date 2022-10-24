// Copyright 2022 The Gitea Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package migrations

import (
	"fmt"

	"code.gitea.io/gitea/models/issues"

	"xorm.io/builder"
	"xorm.io/xorm"
)

func updateOpenMilestoneCounts(x *xorm.Engine) error {
	var openMilestoneIDs []int64
	err := x.Table("milestone").Select("id").Where(builder.Neq{"is_closed": 1}).Find(&openMilestoneIDs)
	if err != nil {
		return fmt.Errorf("error selecting open milestone IDs: %w", err)
	}

	for _, id := range openMilestoneIDs {
		_, err := x.ID(id).
			SetExpr("num_issues", builder.Select("count(*)").From("issue").Where(
				builder.Eq{"milestone_id": id},
			)).
			SetExpr("num_closed_issues", builder.Select("count(*)").From("issue").Where(
				builder.Eq{
					"milestone_id": id,
					"is_closed":    true,
				},
			)).
			Update(&issues.Milestone{})
		if err != nil {
			return fmt.Errorf("error updating issue counts in milestone %d: %w", id, err)
		}
		_, err = x.Exec("UPDATE `milestone` SET completeness=100*num_closed_issues/(CASE WHEN num_issues > 0 THEN num_issues ELSE 1 END) WHERE id=?",
			id,
		)
		if err != nil {
			return fmt.Errorf("error setting completeness on milestone %d: %w", id, err)
		}
	}

	return nil
}
